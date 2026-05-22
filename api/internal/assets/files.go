// Package assets embeds files that are bundled into the API binary
// (course materials, etc.) and are served to authorised users only.
//
// PDF-материалы курсов попадают сюда из репо и отдаются через
// защищённый эндпоинт `/api/courses/:slug/files/:name` — только тем
// пользователям, у кого есть enrollment на соответствующий курс.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed courses/zdorovaya-spina/*.pdf
var courseFiles embed.FS

// CourseFS returns the embedded FS rooted at "courses/".
// Use it like: fs.ReadFile(assets.CourseFS(), "zdorovaya-spina/metodichka.pdf").
func CourseFS() fs.FS {
	sub, err := fs.Sub(courseFiles, "courses")
	if err != nil {
		// embed paths above are static — невозможен здесь по построению
		panic(err)
	}
	return sub
}
