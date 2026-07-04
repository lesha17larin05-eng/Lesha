package db

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID              uuid.UUID
	Email           string
	PasswordHash    string
	Name            string
	Phone           string
	Role            string
	EmailVerifiedAt *time.Time
	LastSeenAt      *time.Time
	CreatedAt       time.Time
}

type Course struct {
	ID                uuid.UUID  `json:"id"`
	Slug              string     `json:"slug"`
	Title             string     `json:"title"`
	Subtitle          string     `json:"subtitle"`
	Description       string     `json:"description"`
	CoverImageURL     string     `json:"cover_image_url"`
	Kind              string     `json:"kind"`
	PriceRub          *int       `json:"price_rub,omitempty"`
	ProdamusProductID *string    `json:"-"`
	IsPublished       bool       `json:"is_published"`
	SortOrder         int        `json:"sort_order"`
}

type Module struct {
	ID        uuid.UUID `json:"id"`
	CourseID  uuid.UUID `json:"course_id"`
	Title     string    `json:"title"`
	SortOrder int       `json:"sort_order"`
}

type Lesson struct {
	ID         uuid.UUID  `json:"id"`
	CourseID   uuid.UUID  `json:"course_id"`
	ModuleID   *uuid.UUID `json:"module_id,omitempty"`
	Title      string     `json:"title"`
	Slug       string     `json:"slug"`
	ContentMD  string     `json:"content_md,omitempty"`
	VideoID    *uuid.UUID `json:"video_id,omitempty"`
	DurationSec int       `json:"duration_sec"`
	SortOrder  int        `json:"sort_order"`
	IsPreview  bool       `json:"is_preview"`
}

type Order struct {
	ID         uuid.UUID
	OrderNum   int64
	UserID     uuid.UUID
	CourseID   uuid.UUID
	AmountRub  int
	Status     string
	PaidAt     *time.Time
	CreatedAt  time.Time
}

type Enrollment struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	CourseID   uuid.UUID
	GrantedBy  string
	GrantedAt  time.Time
}

type Article struct {
	ID             uuid.UUID  `json:"id"`
	Slug           string     `json:"slug"`
	Title          string     `json:"title"`
	Tag            string     `json:"tag"`
	Excerpt        string     `json:"excerpt"`
	CoverImageURL  string     `json:"cover_image_url"`
	ContentHTML    string     `json:"content_html"`
	ReadingMinutes int        `json:"reading_minutes"`
	IsPublished    bool       `json:"is_published"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	SortOrder      int        `json:"sort_order"`
	AuthorID       *uuid.UUID `json:"author_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Video struct {
	ID                uuid.UUID `json:"id"`
	OriginalFilename  string    `json:"original_filename"`
	StoragePath       string    `json:"-"`
	HLSMasterPlaylist string    `json:"hls_master_playlist"`
	DurationSec       int       `json:"duration_sec"`
	SizeBytes         int64     `json:"size_bytes"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}

type Lead struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Contact   string    `json:"contact"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
	Status    string    `json:"status"`
	ConsentPD bool      `json:"consent_pd"`
	CreatedAt time.Time `json:"created_at"`
}
