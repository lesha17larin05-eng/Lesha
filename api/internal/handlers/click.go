package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// Учёт переходов по ссылкам из писем.
//
// Ссылки в письмах цепочки и рассылок ведут не прямо на страницу, а на
// GET /api/c?u=<uuid>&c=<кампания>&l=<ссылка>&t=<подпись>: переход
// записывается в email_clicks, и человек сразу уходит на ссылку (302).
// Переход заодно считается открытием – у многих картинки-счётчики
// заблокированы, а клик точно значит, что письмо открыли.
//
// Подпись (HMAC от кампании, человека и ссылки) не даёт использовать адрес
// как открытый редирект на чужие сайты. С неверной подписью ведём на главную.
const clickPrefix = "click:"

func ClickToken(secret, campaign, userID, link string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(clickPrefix + campaign + ":" + userID + ":" + link))
	return hex.EncodeToString(mac.Sum(nil))
}

// trackURL – ссылка для письма, которая считает переход.
func (a *App) trackURL(campaign string, userID uuid.UUID, link string) string {
	if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
		return link
	}
	return a.Cfg.AppHost + "/api/c?u=" + userID.String() + "&c=" + url.QueryEscape(campaign) +
		"&l=" + url.QueryEscape(link) + "&t=" + ClickToken(a.Cfg.JWTSecret, campaign, userID.String(), link)
}

func (a *App) EmailClick(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rawID, campaign, link, token := q.Get("u"), q.Get("c"), q.Get("l"), q.Get("t")
	dest := "/"
	if id, err := uuid.Parse(rawID); err == nil && campaign != "" && token != "" &&
		(strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "http://")) {
		want := ClickToken(a.Cfg.JWTSecret, campaign, id.String(), link)
		if hmac.Equal([]byte(want), []byte(token)) {
			dest = link
			a.Repo.LogEmailClick(r.Context(), id, campaign, link, r.UserAgent())
			a.Repo.LogEmailOpen(r.Context(), id, campaign, r.UserAgent())
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, dest, http.StatusFound)
}
