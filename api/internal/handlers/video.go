package handlers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leshalarin/api/internal/middleware"
	"github.com/leshalarin/api/internal/video"
)

// AdminUploadVideo — multipart upload, max 5GB.
func (a *App) AdminUploadVideo(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, 400, "bad_form")
		return
	}
	f, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "no_file")
		return
	}
	defer f.Close()
	id, err := a.Repo.CreateVideo(r.Context(), header.Filename)
	if err != nil {
		writeErr(w, 500, "db")
		return
	}
	dir := filepath.Join(a.Cfg.VideoStoragePath, id.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, 500, "mkdir")
		return
	}
	src := filepath.Join(dir, "source"+filepath.Ext(header.Filename))
	out, err := os.Create(src)
	if err != nil {
		writeErr(w, 500, "create")
		return
	}
	size, err := io.Copy(out, f)
	out.Close()
	if err != nil {
		writeErr(w, 500, "copy")
		return
	}
	go a.processVideo(id, dir, src, size)
	writeJSON(w, 202, map[string]any{"id": id, "status": "processing"})
}

func (a *App) processVideo(id uuid.UUID, dir, src string, size int64) {
	ctx := context.Background()
	hls := filepath.Join(dir, "index.m3u8")
	cmd := exec.Command("ffmpeg", "-y", "-i", src,
		"-codec:v", "libx264", "-codec:a", "aac",
		"-hls_time", "10", "-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(dir, "seg_%03d.ts"),
		"-f", "hls", hls)
	if err := cmd.Run(); err != nil {
		slog.Error("ffmpeg failed", "err", err, "video", id)
		_ = a.Repo.UpdateVideoStatus(ctx, id, "failed", src, "", 0, size)
		return
	}
	dur := probeDuration(src)
	_ = a.Repo.UpdateVideoStatus(ctx, id, "ready", hls, "index.m3u8", dur, size)
}

func probeDuration(path string) int {
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries",
		"format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0
	}
	f, err := strconv.ParseFloat(string(trimNewline(out)), 64)
	if err != nil {
		return 0
	}
	return int(f)
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}

// VideoPlayback — issues a short-lived signed token bound to the user.
func (a *App) VideoPlayback(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	vid, err := uuid.Parse(idStr)
	if err != nil {
		writeErr(w, 400, "bad_id")
		return
	}
	v, err := a.Repo.GetVideo(r.Context(), vid)
	if err != nil || v.Status != "ready" {
		writeErr(w, 404, "not_ready")
		return
	}
	uid, _ := middleware.UserID(r.Context())
	courseID, err := a.Repo.VideoCourseID(r.Context(), vid)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	c, err := a.Repo.GetCourseByID(r.Context(), courseID)
	if err != nil {
		writeErr(w, 404, "not_found")
		return
	}
	if c.Kind == "paid" {
		has, _ := a.Repo.HasEnrollment(r.Context(), uid, courseID)
		if !has && middleware.Role(r.Context()) != "admin" {
			writeErr(w, 403, "no_access")
			return
		}
	}
	tok, err := video.IssueToken(a.Cfg.VideoTokenSecret, vid, uid, a.Cfg.VideoTokenTTL)
	if err != nil {
		writeErr(w, 500, "token")
		return
	}
	writeJSON(w, 200, map[string]string{
		"playback_url": fmt.Sprintf("/video-stream/%s/index.m3u8?token=%s", vid.String(), tok),
	})
}

// InternalVideoAuth — called by nginx auth_request.
// Returns 200 with X-Accel-Redirect to /protected-videos/{id}/{file}.
func (a *App) InternalVideoAuth(w http.ResponseWriter, r *http.Request) {
	orig := r.Header.Get("X-Original-URI")
	if orig == "" {
		orig = r.URL.RequestURI()
	}
	// extract /video-stream/{vid}/{rest...}?token=...
	u := orig
	// extract video id from path
	path := u
	if q := indexByte(path, '?'); q >= 0 {
		path = path[:q]
	}
	const prefix = "/video-stream/"
	if !startsWith(path, prefix) {
		w.WriteHeader(400)
		return
	}
	rest := path[len(prefix):]
	slash := indexByte(rest, '/')
	if slash < 0 {
		w.WriteHeader(400)
		return
	}
	vidStr := rest[:slash]
	cookieName := "vt_" + vidStr
	// token from query (initial m3u8 request) or cookie (segments)
	tok := ""
	if i := indexQuery(u, "token="); i >= 0 {
		tok = u[i+6:]
		if amp := indexAmp(tok); amp >= 0 {
			tok = tok[:amp]
		}
	}
	if tok == "" {
		if ck, err := r.Cookie(cookieName); err == nil {
			tok = ck.Value
		}
	}
	if tok == "" {
		w.WriteHeader(401)
		return
	}
	c, err := video.ParseToken(a.Cfg.VideoTokenSecret, tok)
	if err != nil {
		w.WriteHeader(401)
		return
	}
	if vidStr != c.VideoID.String() {
		w.WriteHeader(403)
		return
	}
	// when issuing the manifest, also set a short cookie so segment fetches authenticate.
	if endsWith(path, ".m3u8") {
		ttl := int(a.Cfg.VideoTokenTTL.Seconds())
		http.SetCookie(w, &http.Cookie{
			Name: cookieName, Value: tok,
			Path:     "/video-stream/" + vidStr + "/",
			MaxAge:   ttl,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}
	rel := rest // {vid}/{file}
	w.Header().Set("X-Accel-Redirect", "/protected-videos/"+rel)
	w.Header().Set("Content-Type", "")
	w.WriteHeader(200)
}

func endsWith(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
func indexQuery(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
func indexAmp(s string) int     { return indexByte(s, '&') }
func startsWith(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
