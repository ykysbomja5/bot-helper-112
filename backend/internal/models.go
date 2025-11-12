package internal

import "time"

type User struct {
	ID        int64     `db:"id"`
	TGUserID  int64     `db:"tg_user_id"`
	Username  *string   `db:"username"`
	FirstName *string   `db:"first_name"`
	LastName  *string   `db:"last_name"`
	IsAdmin   bool      `db:"is_admin"`
	CreatedAt time.Time `db:"created_at"`
}

type Chat struct {
	ChatID    int64     `db:"chat_id"`
	Type      string    `db:"type"`
	Title     *string   `db:"title"`
	CreatedAt time.Time `db:"created_at"`
}

type Issue struct {
	ID        int64     `db:"id"`
	UserID    int64     `db:"user_id"`
	ChatID    int64     `db:"chat_id"`
	Text      *string   `db:"text"`
	Latitude  *float64  `db:"latitude"`
	Longitude *float64  `db:"longitude"`
	Status    string    `db:"status"`
	District  *string   `db:"district"`
	Category  *string   `db:"category"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type Attachment struct {
	ID        int64     `db:"id"`
	IssueID   int64     `db:"issue_id"`
	FileID    string    `db:"file_id"`
	FileType  string    `db:"file_type"`
	LocalPath string    `db:"local_path"`
	CreatedAt time.Time `db:"created_at"`
}

type StatusChange struct {
	ID        int64     `db:"id"`
	IssueID   int64     `db:"issue_id"`
	OldStatus *string   `db:"old_status"`
	NewStatus string    `db:"new_status"`
	ChangedBy *int64    `db:"changed_by"`
	Comment   *string   `db:"comment"`
	CreatedAt time.Time `db:"created_at"`
}

type Comment struct {
	ID          int64     `db:"id"`
	IssueID     int64     `db:"issue_id"`
	AdminUserID int64     `db:"admin_user_id"`
	Text        string    `db:"text"`
	CreatedAt   time.Time `db:"created_at"`
}

type ExportRow struct {
	ID        int64     `db:"id"`
	CreatedAt time.Time `db:"created_at"`
	Status    string    `db:"status"`
	UserID    int64     `db:"user_id"`
	TGUserID  int64     `db:"tg_user_id"`
	Text      string    `db:"text"`
	Latitude  *float64  `db:"latitude"`
	Longitude *float64  `db:"longitude"`
}

type WebIssueRequest struct {
	Name        string   `json:"name" binding:"required"`
	Contact     string   `json:"contact" binding:"required"`
	District    string   `json:"district" binding:"required"`
	Category    string   `json:"category" binding:"required"`
	Description string   `json:"description" binding:"required"`
	Latitude    *float64 `json:"latitude,omitempty"`
	Longitude   *float64 `json:"longitude,omitempty"`
	Location    *string  `json:"location,omitempty"`
}

type WebAttachment struct {
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
	FileType string `json:"file_type"`
	FileURL  string `json:"file_url"`
}
