package handlers

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/leshalarin/api/internal/db"
	"github.com/leshalarin/api/internal/middleware"
)

// Настройки сайта – булевы флаги, которые Алексей переключает из админки
// без деплоя (сейчас: показывать ли страницу Салюта).
// Публичный GET /api/settings читает SSR-фронт, PATCH /api/admin/settings –
// только админ, с CSRF и записью в audit_log.

// settingsPayload переводит строковые значения БД в JSON-булевы.
func settingsPayload(raw map[string]string) map[string]bool {
	out := make(map[string]bool, len(raw))
	for k, v := range raw {
		out[k] = v == "true"
	}
	return out
}

func (a *App) PublicSettings(w http.ResponseWriter, r *http.Request) {
	raw, err := a.Repo.Settings(r.Context())
	if err != nil {
		// Дефолты уже в raw – отдаём их, а не 500: из-за настроек
		// не должна падать вся страница.
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, settingsPayload(raw))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, settingsPayload(raw))
}

func (a *App) AdminUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]bool
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_json")
		return
	}
	if len(in) == 0 {
		writeErr(w, 400, "empty")
		return
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		if _, ok := db.SettingDefaults[k]; !ok {
			writeErr(w, 400, "unknown_key")
			return
		}
		keys = append(keys, k)
	}
	sort.Strings(keys) // детерминированный порядок записи и аудита
	adminID, _ := middleware.UserID(r.Context())
	changed := map[string]any{}
	for _, k := range keys {
		val := "false"
		if in[k] {
			val = "true"
		}
		if err := a.Repo.SetSetting(r.Context(), k, val); err != nil {
			writeErr(w, 500, "db")
			return
		}
		changed[k] = in[k]
	}
	a.Repo.Audit(r.Context(), adminID, "settings_update", "settings", nil, changed)
	raw, _ := a.Repo.Settings(r.Context())
	writeJSON(w, 200, settingsPayload(raw))
}
