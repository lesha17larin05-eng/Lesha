package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Repo struct {
	Pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{Pool: pool} }

// ---- USERS ----

func (r *Repo) CreateUser(ctx context.Context, email, hash, name, role string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO users(email,password_hash,name,role) VALUES($1,$2,$3,$4) RETURNING id`,
		email, hash, name, role).Scan(&id)
	return id, err
}

// SetUserPhone сохраняет телефон пользователя. Поле опциональное,
// пустая строка не записывается (NULL).
func (r *Repo) SetUserPhone(ctx context.Context, uid uuid.UUID, phone string) error {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return nil
	}
	_, err := r.Pool.Exec(ctx, `UPDATE users SET phone=$1, updated_at=now() WHERE id=$2`, phone, uid)
	return err
}

func (r *Repo) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	u := &User{}
	err := r.Pool.QueryRow(ctx,
		`SELECT id,email,password_hash,COALESCE(name,''),COALESCE(phone,''),role,email_verified_at,last_seen_at,created_at FROM users WHERE email=$1`,
		email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Phone, &u.Role, &u.EmailVerifiedAt, &u.LastSeenAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (r *Repo) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	u := &User{}
	err := r.Pool.QueryRow(ctx,
		`SELECT id,email,password_hash,COALESCE(name,''),COALESCE(phone,''),role,email_verified_at,last_seen_at,created_at FROM users WHERE id=$1`,
		id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Phone, &u.Role, &u.EmailVerifiedAt, &u.LastSeenAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (r *Repo) MarkEmailVerified(ctx context.Context, userID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `UPDATE users SET email_verified_at=now(), updated_at=now() WHERE id=$1`, userID)
	return err
}

// SaveConsent сохраняет факт согласий пользователя на момент регистрации.
// pd=true → consent_pd_at=now() (обязательное по 152-ФЗ).
// marketing=true → consent_marketing_at=now(); marketing=false → NULL (отзыв или нет согласия).
// Никогда не «сбрасывает» уже выставленный consent_pd_at в NULL — оно держится для аудита.
func (r *Repo) SaveConsent(ctx context.Context, userID uuid.UUID, pd, marketing bool) error {
	var pdSQL string
	if pd {
		pdSQL = `COALESCE(consent_pd_at, now())`
	} else {
		pdSQL = `consent_pd_at`
	}
	marketingSQL := `NULL`
	if marketing {
		marketingSQL = `now()`
	}
	_, err := r.Pool.Exec(ctx,
		`UPDATE users SET consent_pd_at=`+pdSQL+`, consent_marketing_at=`+marketingSQL+`, updated_at=now() WHERE id=$1`,
		userID)
	return err
}

func (r *Repo) UpdateUserName(ctx context.Context, userID uuid.UUID, name string) error {
	_, err := r.Pool.Exec(ctx, `UPDATE users SET name=$1, updated_at=now() WHERE id=$2`, name, userID)
	return err
}

func (r *Repo) UpdateUserPassword(ctx context.Context, userID uuid.UUID, hash string) error {
	_, err := r.Pool.Exec(ctx, `UPDATE users SET password_hash=$1, updated_at=now() WHERE id=$2`, hash, userID)
	return err
}

func (r *Repo) TouchLastSeen(ctx context.Context, userID uuid.UUID) {
	_, _ = r.Pool.Exec(ctx, `UPDATE users SET last_seen_at=now() WHERE id=$1 AND (last_seen_at IS NULL OR last_seen_at < now() - interval '60 seconds')`, userID)
}

func (r *Repo) ListUsers(ctx context.Context, search string, limit, offset int) ([]*User, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id,email,'',COALESCE(name,''),COALESCE(phone,''),role,email_verified_at,last_seen_at,created_at FROM users
		 WHERE ($1='' OR email ILIKE '%'||$1||'%' OR name ILIKE '%'||$1||'%' OR phone ILIKE '%'||$1||'%')
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, search, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Phone, &u.Role, &u.EmailVerifiedAt, &u.LastSeenAt, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func (r *Repo) CountOnlineUsers(ctx context.Context, since time.Duration) (int, error) {
	var n int
	err := r.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE last_seen_at > now() - $1::interval`, since.String()).Scan(&n)
	return n, err
}

// ---- TOKENS ----

func (r *Repo) CreateEmailToken(ctx context.Context, userID uuid.UUID, hash string, ttl time.Duration) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO email_verification_tokens(user_id,token_hash,expires_at) VALUES($1,$2,$3)`,
		userID, hash, time.Now().Add(ttl))
	return err
}

func (r *Repo) UseEmailToken(ctx context.Context, hash string) (uuid.UUID, error) {
	var uid uuid.UUID
	err := r.Pool.QueryRow(ctx,
		`UPDATE email_verification_tokens SET used_at=now() WHERE token_hash=$1 AND used_at IS NULL AND expires_at>now() RETURNING user_id`,
		hash).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return uid, err
}

func (r *Repo) CreatePasswordResetToken(ctx context.Context, userID uuid.UUID, hash string, ttl time.Duration) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO password_reset_tokens(user_id,token_hash,expires_at) VALUES($1,$2,$3)`,
		userID, hash, time.Now().Add(ttl))
	return err
}

func (r *Repo) UsePasswordResetToken(ctx context.Context, hash string) (uuid.UUID, error) {
	var uid uuid.UUID
	err := r.Pool.QueryRow(ctx,
		`UPDATE password_reset_tokens SET used_at=now() WHERE token_hash=$1 AND used_at IS NULL AND expires_at>now() RETURNING user_id`,
		hash).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return uid, err
}

// ---- SESSIONS ----

func (r *Repo) CreateSession(ctx context.Context, userID uuid.UUID, hash, ua, ip string, ttl time.Duration) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO sessions(user_id,refresh_token_hash,user_agent,ip,expires_at) VALUES($1,$2,$3,$4,$5)`,
		userID, hash, ua, ip, time.Now().Add(ttl))
	return err
}

func (r *Repo) FindSession(ctx context.Context, hash string) (uuid.UUID, error) {
	var uid uuid.UUID
	err := r.Pool.QueryRow(ctx,
		`SELECT user_id FROM sessions WHERE refresh_token_hash=$1 AND revoked_at IS NULL AND expires_at>now()`,
		hash).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return uid, err
}

func (r *Repo) RevokeSession(ctx context.Context, hash string) error {
	_, err := r.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE refresh_token_hash=$1`, hash)
	return err
}

// ---- COURSES ----

func (r *Repo) ListCourses(ctx context.Context, onlyPublished bool) ([]*Course, error) {
	q := `SELECT id,slug,title,COALESCE(subtitle,''),COALESCE(description,''),COALESCE(cover_image_url,''),kind,price_rub,prodamus_product_id,is_published,sort_order
	      FROM courses`
	if onlyPublished {
		q += ` WHERE is_published=true`
	}
	q += ` ORDER BY sort_order, created_at`
	rows, err := r.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Course
	for rows.Next() {
		c := &Course{}
		if err := rows.Scan(&c.ID, &c.Slug, &c.Title, &c.Subtitle, &c.Description, &c.CoverImageURL, &c.Kind, &c.PriceRub, &c.ProdamusProductID, &c.IsPublished, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *Repo) GetCourseBySlug(ctx context.Context, slug string) (*Course, error) {
	c := &Course{}
	err := r.Pool.QueryRow(ctx,
		`SELECT id,slug,title,COALESCE(subtitle,''),COALESCE(description,''),COALESCE(cover_image_url,''),kind,price_rub,prodamus_product_id,is_published,sort_order
		 FROM courses WHERE slug=$1`,
		slug).Scan(&c.ID, &c.Slug, &c.Title, &c.Subtitle, &c.Description, &c.CoverImageURL, &c.Kind, &c.PriceRub, &c.ProdamusProductID, &c.IsPublished, &c.SortOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func (r *Repo) GetCourseByID(ctx context.Context, id uuid.UUID) (*Course, error) {
	c := &Course{}
	err := r.Pool.QueryRow(ctx,
		`SELECT id,slug,title,COALESCE(subtitle,''),COALESCE(description,''),COALESCE(cover_image_url,''),kind,price_rub,prodamus_product_id,is_published,sort_order
		 FROM courses WHERE id=$1`,
		id).Scan(&c.ID, &c.Slug, &c.Title, &c.Subtitle, &c.Description, &c.CoverImageURL, &c.Kind, &c.PriceRub, &c.ProdamusProductID, &c.IsPublished, &c.SortOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

type CourseInput struct {
	Slug, Title, Subtitle, Description, CoverImageURL, Kind string
	PriceRub                                                *int
	IsPublished                                             bool
	SortOrder                                               int
}

func (r *Repo) CreateCourse(ctx context.Context, in CourseInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO courses(slug,title,subtitle,description,cover_image_url,kind,price_rub,is_published,sort_order)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		in.Slug, in.Title, in.Subtitle, in.Description, in.CoverImageURL, in.Kind, in.PriceRub, in.IsPublished, in.SortOrder).Scan(&id)
	return id, err
}

func (r *Repo) UpdateCourse(ctx context.Context, id uuid.UUID, in CourseInput) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE courses SET slug=$1,title=$2,subtitle=$3,description=$4,cover_image_url=$5,kind=$6,price_rub=$7,is_published=$8,sort_order=$9,updated_at=now()
		 WHERE id=$10`,
		in.Slug, in.Title, in.Subtitle, in.Description, in.CoverImageURL, in.Kind, in.PriceRub, in.IsPublished, in.SortOrder, id)
	return err
}

func (r *Repo) DeleteCourse(ctx context.Context, id uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM courses WHERE id=$1`, id)
	return err
}

// ---- MODULES & LESSONS ----

func (r *Repo) ListModules(ctx context.Context, courseID uuid.UUID) ([]*Module, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id,course_id,title,sort_order FROM modules WHERE course_id=$1 ORDER BY sort_order`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Module
	for rows.Next() {
		m := &Module{}
		if err := rows.Scan(&m.ID, &m.CourseID, &m.Title, &m.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (r *Repo) CreateModule(ctx context.Context, courseID uuid.UUID, title string, sortOrder int) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.Pool.QueryRow(ctx, `INSERT INTO modules(course_id,title,sort_order) VALUES($1,$2,$3) RETURNING id`,
		courseID, title, sortOrder).Scan(&id)
	return id, err
}

func (r *Repo) UpdateModule(ctx context.Context, id uuid.UUID, title string, sortOrder int) error {
	_, err := r.Pool.Exec(ctx, `UPDATE modules SET title=$1, sort_order=$2 WHERE id=$3`, title, sortOrder, id)
	return err
}

func (r *Repo) ListLessons(ctx context.Context, courseID uuid.UUID) ([]*Lesson, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id,course_id,module_id,title,slug,COALESCE(content_md,''),video_id,COALESCE(duration_sec,0),sort_order,is_preview
		 FROM lessons WHERE course_id=$1 ORDER BY sort_order`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Lesson
	for rows.Next() {
		l := &Lesson{}
		if err := rows.Scan(&l.ID, &l.CourseID, &l.ModuleID, &l.Title, &l.Slug, &l.ContentMD, &l.VideoID, &l.DurationSec, &l.SortOrder, &l.IsPreview); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func (r *Repo) GetLesson(ctx context.Context, courseID uuid.UUID, slug string) (*Lesson, error) {
	l := &Lesson{}
	err := r.Pool.QueryRow(ctx,
		`SELECT id,course_id,module_id,title,slug,COALESCE(content_md,''),video_id,COALESCE(duration_sec,0),sort_order,is_preview
		 FROM lessons WHERE course_id=$1 AND slug=$2`, courseID, slug).
		Scan(&l.ID, &l.CourseID, &l.ModuleID, &l.Title, &l.Slug, &l.ContentMD, &l.VideoID, &l.DurationSec, &l.SortOrder, &l.IsPreview)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return l, err
}

func (r *Repo) GetLessonByID(ctx context.Context, id uuid.UUID) (*Lesson, error) {
	l := &Lesson{}
	err := r.Pool.QueryRow(ctx,
		`SELECT id,course_id,module_id,title,slug,COALESCE(content_md,''),video_id,COALESCE(duration_sec,0),sort_order,is_preview
		 FROM lessons WHERE id=$1`, id).
		Scan(&l.ID, &l.CourseID, &l.ModuleID, &l.Title, &l.Slug, &l.ContentMD, &l.VideoID, &l.DurationSec, &l.SortOrder, &l.IsPreview)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return l, err
}

type LessonInput struct {
	CourseID    uuid.UUID
	ModuleID    *uuid.UUID
	Title, Slug string
	ContentMD   string
	VideoID     *uuid.UUID
	DurationSec int
	SortOrder   int
	IsPreview   bool
}

func (r *Repo) CreateLesson(ctx context.Context, in LessonInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO lessons(course_id,module_id,title,slug,content_md,video_id,duration_sec,sort_order,is_preview)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		in.CourseID, in.ModuleID, in.Title, in.Slug, in.ContentMD, in.VideoID, in.DurationSec, in.SortOrder, in.IsPreview).Scan(&id)
	return id, err
}

func (r *Repo) UpdateLesson(ctx context.Context, id uuid.UUID, in LessonInput) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE lessons SET module_id=$1,title=$2,slug=$3,content_md=$4,video_id=$5,duration_sec=$6,sort_order=$7,is_preview=$8 WHERE id=$9`,
		in.ModuleID, in.Title, in.Slug, in.ContentMD, in.VideoID, in.DurationSec, in.SortOrder, in.IsPreview, id)
	return err
}

func (r *Repo) DeleteLesson(ctx context.Context, id uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM lessons WHERE id=$1`, id)
	return err
}

// ---- ENROLLMENTS ----

func (r *Repo) Grant(ctx context.Context, userID, courseID uuid.UUID, by string, adminID *uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO enrollments(user_id,course_id,granted_by,granted_by_admin_id) VALUES($1,$2,$3,$4)
		 ON CONFLICT (user_id,course_id) DO NOTHING`,
		userID, courseID, by, adminID)
	return err
}

func (r *Repo) Revoke(ctx context.Context, userID, courseID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM enrollments WHERE user_id=$1 AND course_id=$2`, userID, courseID)
	return err
}

func (r *Repo) HasEnrollment(ctx context.Context, userID, courseID uuid.UUID) (bool, error) {
	var n int
	err := r.Pool.QueryRow(ctx, `SELECT count(*) FROM enrollments WHERE user_id=$1 AND course_id=$2`, userID, courseID).Scan(&n)
	return n > 0, err
}

func (r *Repo) UserEnrollments(ctx context.Context, userID uuid.UUID) ([]*Course, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT c.id,c.slug,c.title,COALESCE(c.subtitle,''),COALESCE(c.description,''),COALESCE(c.cover_image_url,''),c.kind,c.price_rub,c.prodamus_product_id,c.is_published,c.sort_order
		 FROM courses c JOIN enrollments e ON e.course_id=c.id WHERE e.user_id=$1 ORDER BY e.granted_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Course
	for rows.Next() {
		c := &Course{}
		if err := rows.Scan(&c.ID, &c.Slug, &c.Title, &c.Subtitle, &c.Description, &c.CoverImageURL, &c.Kind, &c.PriceRub, &c.ProdamusProductID, &c.IsPublished, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// ---- PROGRESS ----

func (r *Repo) UpsertProgress(ctx context.Context, userID, lessonID uuid.UUID, completed bool, pos int) error {
	var completedAt any
	if completed {
		completedAt = time.Now()
	}
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO lesson_progress(user_id,lesson_id,completed_at,last_position_sec)
		 VALUES($1,$2,$3,$4)
		 ON CONFLICT (user_id,lesson_id) DO UPDATE
		 SET completed_at=COALESCE(lesson_progress.completed_at,EXCLUDED.completed_at),
		     last_position_sec=GREATEST(lesson_progress.last_position_sec, EXCLUDED.last_position_sec),
		     updated_at=now()`,
		userID, lessonID, completedAt, pos)
	return err
}

func (r *Repo) CourseProgress(ctx context.Context, userID, courseID uuid.UUID) (total, done int, err error) {
	err = r.Pool.QueryRow(ctx,
		`SELECT
		   (SELECT count(*) FROM lessons WHERE course_id=$2),
		   (SELECT count(*) FROM lesson_progress lp JOIN lessons l ON l.id=lp.lesson_id WHERE lp.user_id=$1 AND l.course_id=$2 AND lp.completed_at IS NOT NULL)`,
		userID, courseID).Scan(&total, &done)
	return
}

// ---- ORDERS ----

func (r *Repo) CreateOrder(ctx context.Context, userID, courseID uuid.UUID, amount int) (*Order, error) {
	o := &Order{}
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO orders(user_id,course_id,amount_rub,status) VALUES($1,$2,$3,'pending')
		 RETURNING id,order_num,user_id,course_id,amount_rub,status,paid_at,created_at`,
		userID, courseID, amount).Scan(&o.ID, &o.OrderNum, &o.UserID, &o.CourseID, &o.AmountRub, &o.Status, &o.PaidAt, &o.CreatedAt)
	return o, err
}

func (r *Repo) GetOrder(ctx context.Context, id uuid.UUID) (*Order, error) {
	o := &Order{}
	err := r.Pool.QueryRow(ctx,
		`SELECT id,order_num,user_id,course_id,amount_rub,status,paid_at,created_at FROM orders WHERE id=$1`, id).
		Scan(&o.ID, &o.OrderNum, &o.UserID, &o.CourseID, &o.AmountRub, &o.Status, &o.PaidAt, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return o, err
}

func (r *Repo) GetOrderByNum(ctx context.Context, num int64) (*Order, error) {
	o := &Order{}
	err := r.Pool.QueryRow(ctx,
		`SELECT id,order_num,user_id,course_id,amount_rub,status,paid_at,created_at FROM orders WHERE order_num=$1`, num).
		Scan(&o.ID, &o.OrderNum, &o.UserID, &o.CourseID, &o.AmountRub, &o.Status, &o.PaidAt, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return o, err
}

func (r *Repo) MarkOrderPaid(ctx context.Context, orderID uuid.UUID, prodamusOrderID string) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE orders SET status='paid', paid_at=now(), prodamus_order_id=$2 WHERE id=$1 AND status='pending'`,
		orderID, prodamusOrderID)
	return err
}

func (r *Repo) ListOrders(ctx context.Context, status string, from, to *time.Time, limit, offset int) ([]*Order, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id,order_num,user_id,course_id,amount_rub,status,paid_at,created_at FROM orders
		 WHERE ($1='' OR status=$1) AND ($2::timestamptz IS NULL OR created_at>=$2) AND ($3::timestamptz IS NULL OR created_at<=$3)
		 ORDER BY created_at DESC LIMIT $4 OFFSET $5`, status, from, to, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Order
	for rows.Next() {
		o := &Order{}
		if err := rows.Scan(&o.ID, &o.OrderNum, &o.UserID, &o.CourseID, &o.AmountRub, &o.Status, &o.PaidAt, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

func (r *Repo) RevenueSince(ctx context.Context, since time.Time) (int, error) {
	var n *int
	err := r.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_rub),0) FROM orders WHERE status='paid' AND paid_at>$1`, since).Scan(&n)
	if n == nil {
		return 0, err
	}
	return *n, err
}

// ---- VIDEOS ----

func (r *Repo) CreateVideo(ctx context.Context, filename string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO videos(original_filename,status) VALUES($1,'uploading') RETURNING id`,
		filename).Scan(&id)
	return id, err
}

func (r *Repo) UpdateVideoStatus(ctx context.Context, id uuid.UUID, status, storagePath, hls string, duration int, size int64) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE videos SET status=$1, storage_path=$2, hls_master_playlist=$3, duration_sec=$4, size_bytes=$5 WHERE id=$6`,
		status, storagePath, hls, duration, size, id)
	return err
}

func (r *Repo) GetVideo(ctx context.Context, id uuid.UUID) (*Video, error) {
	v := &Video{}
	err := r.Pool.QueryRow(ctx,
		`SELECT id,COALESCE(original_filename,''),COALESCE(storage_path,''),COALESCE(hls_master_playlist,''),COALESCE(duration_sec,0),COALESCE(size_bytes,0),status,created_at
		 FROM videos WHERE id=$1`, id).
		Scan(&v.ID, &v.OriginalFilename, &v.StoragePath, &v.HLSMasterPlaylist, &v.DurationSec, &v.SizeBytes, &v.Status, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

// VideoCourseID finds the course that owns this video (via lessons.video_id).
func (r *Repo) VideoCourseID(ctx context.Context, videoID uuid.UUID) (uuid.UUID, error) {
	var cid uuid.UUID
	err := r.Pool.QueryRow(ctx, `SELECT course_id FROM lessons WHERE video_id=$1 LIMIT 1`, videoID).Scan(&cid)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return cid, err
}

// ---- AUDIT ----

func (r *Repo) Audit(ctx context.Context, adminID uuid.UUID, action, targetType string, targetID *uuid.UUID, meta map[string]any) {
	_, _ = r.Pool.Exec(ctx,
		`INSERT INTO audit_log(admin_id,action,target_type,target_id,meta) VALUES($1,$2,$3,$4,$5)`,
		adminID, action, targetType, targetID, meta)
}

// ---- PAYMENT WEBHOOKS ----

func (r *Repo) LogWebhook(ctx context.Context, headers, body map[string]any, sigValid bool) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO payment_webhooks(headers,body,signature_valid) VALUES($1,$2,$3) RETURNING id`,
		headers, body, sigValid).Scan(&id)
	return id, err
}

func (r *Repo) MarkWebhookProcessed(ctx context.Context, id uuid.UUID, orderID *uuid.UUID, errMsg string) {
	_, _ = r.Pool.Exec(ctx,
		`UPDATE payment_webhooks SET processed=true, processing_error=NULLIF($2,''), order_id=$3 WHERE id=$1`,
		id, errMsg, orderID)
}

// ---- LEADS ----

func (r *Repo) CreateLead(ctx context.Context, name, contact, message, source string, consentPD bool) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.Pool.QueryRow(ctx,
		`INSERT INTO leads(name,contact,message,source,consent_pd) VALUES($1,$2,$3,$4,$5) RETURNING id`,
		name, contact, message, source, consentPD).Scan(&id)
	return id, err
}

func (r *Repo) ListLeads(ctx context.Context, limit int) ([]Lead, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id,name,contact,message,source,status,consent_pd,created_at
		   FROM leads ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lead
	for rows.Next() {
		var l Lead
		if err := rows.Scan(&l.ID, &l.Name, &l.Contact, &l.Message, &l.Source, &l.Status, &l.ConsentPD, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *Repo) UpdateLeadStatus(ctx context.Context, id uuid.UUID, status string) error {
	ct, err := r.Pool.Exec(ctx, `UPDATE leads SET status=$2 WHERE id=$1`, id, status)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- ADMIN: карточка пользователя ----

// UserConsents — даты согласий (152-ФЗ) для карточки пользователя в админке.
func (r *Repo) UserConsents(ctx context.Context, userID uuid.UUID) (pdAt, marketingAt *time.Time, err error) {
	err = r.Pool.QueryRow(ctx,
		`SELECT consent_pd_at, consent_marketing_at FROM users WHERE id=$1`, userID).Scan(&pdAt, &marketingAt)
	return
}

// UserEnrollmentInfo — доступ пользователя к курсу: кто/когда выдал.
type UserEnrollmentInfo struct {
	CourseID  uuid.UUID `json:"course_id"`
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	GrantedBy string    `json:"granted_by"` // purchase | admin | free
	GrantedAt time.Time `json:"granted_at"`
}

func (r *Repo) UserEnrollmentsInfo(ctx context.Context, userID uuid.UUID) ([]UserEnrollmentInfo, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT c.id, c.slug, c.title, c.kind, e.granted_by, e.granted_at
		   FROM enrollments e JOIN courses c ON c.id = e.course_id
		  WHERE e.user_id = $1 ORDER BY e.granted_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserEnrollmentInfo
	for rows.Next() {
		var e UserEnrollmentInfo
		if err := rows.Scan(&e.CourseID, &e.Slug, &e.Title, &e.Kind, &e.GrantedBy, &e.GrantedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UserLessonState — состояние одного урока у пользователя (для карточки в админке).
type UserLessonState struct {
	LessonID        uuid.UUID  `json:"lesson_id"`
	Title           string     `json:"title"`
	SortOrder       int        `json:"sort_order"`
	DurationSec     int        `json:"duration_sec"`
	Completed       bool       `json:"completed"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	LastPositionSec int        `json:"last_position_sec"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

func (r *Repo) UserCourseLessons(ctx context.Context, userID, courseID uuid.UUID) ([]UserLessonState, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT l.id, l.title, l.sort_order, l.duration_sec,
		        (p.completed_at IS NOT NULL) AS completed, p.completed_at,
		        COALESCE(p.last_position_sec, 0), p.updated_at
		   FROM lessons l
		   LEFT JOIN lesson_progress p ON p.lesson_id = l.id AND p.user_id = $1
		  WHERE l.course_id = $2 ORDER BY l.sort_order`, userID, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserLessonState
	for rows.Next() {
		var s UserLessonState
		if err := rows.Scan(&s.LessonID, &s.Title, &s.SortOrder, &s.DurationSec, &s.Completed, &s.CompletedAt, &s.LastPositionSec, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// UserOrderInfo — заказ пользователя с названием курса (для карточки в админке).
type UserOrderInfo struct {
	ID          uuid.UUID  `json:"id"`
	OrderNum    int64      `json:"order_num"`
	CourseTitle string     `json:"course_title"`
	AmountRub   int        `json:"amount_rub"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	PaidAt      *time.Time `json:"paid_at,omitempty"`
}

func (r *Repo) OrdersByUser(ctx context.Context, userID uuid.UUID) ([]UserOrderInfo, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT o.id, o.order_num, c.title, o.amount_rub, o.status, o.created_at, o.paid_at
		   FROM orders o JOIN courses c ON c.id = o.course_id
		  WHERE o.user_id = $1 ORDER BY o.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserOrderInfo
	for rows.Next() {
		var o UserOrderInfo
		if err := rows.Scan(&o.ID, &o.OrderNum, &o.CourseTitle, &o.AmountRub, &o.Status, &o.CreatedAt, &o.PaidAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
