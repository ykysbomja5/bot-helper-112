package internal

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Pool *pgxpool.Pool
}

func NewDB(ctx context.Context, url string) *DB {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		log.Fatalf("Ошибка подключения к БД: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("Ошибка пинга БД: %v", err)
	}

	log.Println("Подключение к базе данных успешно установлено")
	return &DB{Pool: pool}
}

func (db *DB) Close() {
	db.Pool.Close()
}

// создание таблиц если их нет
func (db *DB) InitSchema(ctx context.Context) error {
	if err := db.Pool.Ping(ctx); err != nil {
		return fmt.Errorf("ошибка соединения с базой: %w", err)
	}
	log.Println("Проверка соединения с базой данных... OK")

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id bigserial PRIMARY KEY,
		tg_user_id bigint UNIQUE NOT NULL,
		username text,
		first_name text,
		last_name text,
		is_admin boolean NOT NULL DEFAULT false,
		created_at timestamptz NOT NULL DEFAULT now()
	);

	CREATE TABLE IF NOT EXISTS chats (
		chat_id bigint PRIMARY KEY,
		type text NOT NULL,
		title text,
		created_at timestamptz NOT NULL DEFAULT now()
	);

	CREATE TABLE IF NOT EXISTS issues (
		id bigserial PRIMARY KEY,
		user_id bigint NOT NULL,
		chat_id bigint NOT NULL,
		text text,
		latitude double precision,
		longitude double precision,
		status text NOT NULL DEFAULT 'Новая',
		district text,
		category text,
		created_at timestamptz NOT NULL DEFAULT now(),
		updated_at timestamptz NOT NULL DEFAULT now()
	);

	CREATE INDEX IF NOT EXISTS idx_issues_status ON issues(status);
	CREATE INDEX IF NOT EXISTS idx_issues_created_at ON issues(created_at);

	CREATE TABLE IF NOT EXISTS attachments (
		id bigserial PRIMARY KEY,
		issue_id bigint NOT NULL,
		file_id text NOT NULL,
		file_type text NOT NULL,
		local_path text NOT NULL,
		created_at timestamptz NOT NULL DEFAULT now()
	);

	CREATE TABLE IF NOT EXISTS status_changes (
		id bigserial PRIMARY KEY,
		issue_id bigint NOT NULL,
		old_status text,
		new_status text NOT NULL,
		changed_by bigint,
		comment text,
		created_at timestamptz NOT NULL DEFAULT now()
	);

	CREATE TABLE IF NOT EXISTS comments (
		id bigserial PRIMARY KEY,
		issue_id bigint NOT NULL,
		admin_user_id bigint NOT NULL,
		text text NOT NULL,
		created_at timestamptz NOT NULL DEFAULT now()
	);

	CREATE TABLE IF NOT EXISTS broadcasts (
		id bigserial PRIMARY KEY,
		text text NOT NULL,
		created_by bigint,
		sent_count int NOT NULL DEFAULT 0,
		created_at timestamptz NOT NULL DEFAULT now()
	);
	`

	if _, err := db.Pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("ошибка при инициализации схемы: %w", err)
	}

	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO users (id, tg_user_id, username, first_name, last_name, is_admin)
		VALUES (1, 1, 'web_user', 'Web', 'User', false)
		ON CONFLICT (id) DO NOTHING;
	`); err != nil {
		return fmt.Errorf("failed to create default user: %w", err)
	}

	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO chats (chat_id, type, title)
		VALUES (1, 'web', 'Web Issues')
		ON CONFLICT (chat_id) DO NOTHING;
	`); err != nil {
		return fmt.Errorf("failed to create default chat: %w", err)
	}

	log.Println("Схема базы данных успешно инициализирована")
	return nil
}

func (db *DB) CreateWebIssue(ctx context.Context, req *WebIssueRequest) (*Issue, error) {
	webUserID := int64(1)
	webChatID := int64(1)

	name := strings.TrimSpace(req.Name)
	contact := strings.TrimSpace(req.Contact)
	desc := strings.TrimSpace(req.Description)

	var lines []string

	if name != "" {
		lines = append(lines, "Имя: "+name)
	}
	if contact != "" {
		lines = append(lines, "Контакт: "+contact)
	}

	if desc != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "Описание проблемы:")
		lines = append(lines, desc)
	} else {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "Описание проблемы:")
		lines = append(lines, "(не заполнено)")
	}

	text := strings.Join(lines, "\n")

	iss := &Issue{
		UserID:    webUserID,
		ChatID:    webChatID,
		Text:      &text,
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
		Status:    "Новая",
		District:  &req.District,
		Category:  &req.Category,
	}

	issue, err := db.CreateIssue(ctx, iss)
	if err != nil {
		return nil, fmt.Errorf("create web issue: %w", err)
	}

	log.Printf("Новая веб-заявка ID=%d (%s / %s)", issue.ID, req.District, req.Category)
	return issue, nil
}

func (db *DB) AddWebAttachment(ctx context.Context, issueID int64, fileName, fileType, fileURL string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO attachments (issue_id, file_id, file_type, local_path)
		VALUES ($1, $2, $3, $4)
	`, issueID, fileName, fileType, fileURL)

	if err != nil {
		return fmt.Errorf("ошибка при добавлении вложения: %w", err)
	}

	log.Printf("📎 Вложение добавлено: %s (%s)", fileName, fileType)
	return nil
}

func (db *DB) GetWebIssueByID(ctx context.Context, issueID int64) (*Issue, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT id, user_id, chat_id, text, latitude, longitude, status, district, category, created_at, updated_at
		FROM issues WHERE id = $1
	`, issueID)

	var issue Issue
	if err := row.Scan(&issue.ID, &issue.UserID, &issue.ChatID, &issue.Text,
		&issue.Latitude, &issue.Longitude, &issue.Status, &issue.District,
		&issue.Category, &issue.CreatedAt, &issue.UpdatedAt); err != nil {
		return nil, err
	}
	return &issue, nil
}

func (db *DB) GetAttachmentsByIssueID(ctx context.Context, issueID int64) ([]Attachment, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, issue_id, file_id, file_type, local_path, created_at
		FROM attachments WHERE issue_id = $1 ORDER BY created_at
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []Attachment
	for rows.Next() {
		var att Attachment
		if err := rows.Scan(&att.ID, &att.IssueID, &att.FileID, &att.FileType, &att.LocalPath, &att.CreatedAt); err != nil {
			return nil, err
		}
		attachments = append(attachments, att)
	}
	return attachments, nil
}

func (db *DB) UpsertUser(ctx context.Context, u *User) (*User, error) {
	row := db.Pool.QueryRow(ctx, `
		INSERT INTO users (tg_user_id, username, first_name, last_name, is_admin)
		VALUES ($1,$2,$3,$4,COALESCE($5,false))
		ON CONFLICT (tg_user_id) DO UPDATE SET
			username=excluded.username,
			first_name=excluded.first_name,
			last_name=excluded.last_name
		RETURNING id, is_admin, created_at
	`, u.TGUserID, u.Username, u.FirstName, u.LastName, u.IsAdmin)

	if err := row.Scan(&u.ID, &u.IsAdmin, &u.CreatedAt); err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) UpsertChat(ctx context.Context, c *Chat) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO chats (chat_id, type, title)
		VALUES ($1,$2,$3)
		ON CONFLICT(chat_id) DO UPDATE SET type=excluded.type, title=excluded.title
	`, c.ChatID, c.Type, c.Title)
	return err
}

func (db *DB) PromoteToAdmin(ctx context.Context, tgUserID int64) error {
	cmd, err := db.Pool.Exec(ctx, `UPDATE users SET is_admin=true WHERE tg_user_id=$1`, tgUserID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errors.New("user not found to promote")
	}
	return nil
}

func (db *DB) IsAdmin(ctx context.Context, tgUserID int64) (bool, error) {
	row := db.Pool.QueryRow(ctx, `SELECT is_admin FROM users WHERE tg_user_id=$1`, tgUserID)
	var isAdmin bool
	switch err := row.Scan(&isAdmin); err {
	case nil:
		return isAdmin, nil
	case pgx.ErrNoRows:
		return false, nil
	default:
		return false, err
	}
}

func (db *DB) CreateIssue(ctx context.Context, iss *Issue) (*Issue, error) {
	if iss.Status == "" {
		iss.Status = "Новая"
	}

	row := db.Pool.QueryRow(ctx, `
        insert into issues (user_id, chat_id, text, latitude, longitude, status, district, category)
        values ($1,$2,$3,$4,$5,$6,$7,$8)
        returning id, created_at, updated_at
    `,
		iss.UserID,
		iss.ChatID,
		iss.Text,
		iss.Latitude,
		iss.Longitude,
		iss.Status,
		iss.District,
		iss.Category,
	)

	if err := row.Scan(&iss.ID, &iss.CreatedAt, &iss.UpdatedAt); err != nil {
		return nil, err
	}
	return iss, nil
}

func (db *DB) AddAttachment(ctx context.Context, a *Attachment) error {
	row := db.Pool.QueryRow(ctx, `
        insert into attachments (issue_id, file_id, file_type, local_path)
        values ($1,$2,$3,$4)
        returning id, created_at
    `, a.IssueID, a.FileID, a.FileType, a.LocalPath)
	return row.Scan(&a.ID, &a.CreatedAt)
}

func (db *DB) ListIssuesByUser(ctx context.Context, userID int64, limit int) ([]Issue, error) {
	rows, err := db.Pool.Query(ctx, `
		select id, user_id, chat_id, text, latitude, longitude, status, district, category created_at, updated_at
		from issues where user_id=$1 order by created_at desc limit $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []Issue
	for rows.Next() {
		var x Issue
		if err := rows.Scan(&x.ID, &x.UserID, &x.ChatID, &x.Text,
			&x.Latitude, &x.Longitude, &x.Status,
			&x.District, &x.Category,
			&x.CreatedAt, &x.UpdatedAt,
		); err != nil {
			return nil, err
		}
		res = append(res, x)
	}
	return res, rows.Err()
}

func (db *DB) ListIssuesByStatus(ctx context.Context, statuses []string, limit int) ([]Issue, error) {
	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses)+1)
	for i, s := range statuses {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = s
	}
	args[len(args)-1] = limit
	q := fmt.Sprintf(`
		select id, user_id, chat_id, text, latitude, longitude, status, district, category, created_at, updated_at
		from issues where status in (%s) order by created_at desc limit $%d
	`, strings.Join(placeholders, ","), len(args))
	rows, err := db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []Issue
	for rows.Next() {
		var x Issue
		if err := rows.Scan(&x.ID, &x.UserID, &x.ChatID, &x.Text,
			&x.Latitude, &x.Longitude, &x.Status,
			&x.District, &x.Category,
			&x.CreatedAt, &x.UpdatedAt,
		); err != nil {
			return nil, err
		}
		res = append(res, x)
	}
	return res, rows.Err()
}

func (db *DB) SetIssueStatus(ctx context.Context, issueID int64, newStatus string, changedByTG *int64, comment *string) error {
	row := db.Pool.QueryRow(ctx, `select status from issues where id=$1`, issueID)

	var oldStatus *string
	var os string
	switch err := row.Scan(&os); err {
	case nil:
		oldStatus = &os
	case pgx.ErrNoRows:
		return errors.New("issue not found")
	default:
		return err
	}

	var changedByID *int64
	if changedByTG != nil {
		r2 := db.Pool.QueryRow(ctx, `select id from users where tg_user_id=$1`, *changedByTG)
		var id int64
		if err := r2.Scan(&id); err == nil {
			changedByID = &id
		}
	}

	if _, err := db.Pool.Exec(ctx,
		`update issues set status=$2, updated_at=now() where id=$1`,
		issueID, newStatus,
	); err != nil {
		return err
	}

	if _, err := db.Pool.Exec(ctx, `
        insert into status_changes(issue_id, old_status, new_status, changed_by, comment)
        values ($1,$2,$3,$4,$5)
    `, issueID, oldStatus, newStatus, changedByID, comment); err != nil {
		return err
	}

	return nil
}

func (db *DB) AddComment(ctx context.Context, issueID int64, adminTGUserID int64, text string) error {
	_, err := db.Pool.Exec(ctx, `
        insert into comments(issue_id, admin_user_id, text)
        select $1, id, $3
        from users
        where tg_user_id = $2
    `, issueID, adminTGUserID, text)
	return err
}

func (db *DB) ExportIssues(ctx context.Context, from, to time.Time) ([]ExportRow, error) {
	rows, err := db.Pool.Query(ctx, `
		select i.id, i.created_at, i.status, i.user_id, u.tg_user_id, coalesce(i.text,''), i.latitude, i.longitude
		from issues i
		join users u on u.id = i.user_id
		where i.created_at >= $1 and i.created_at < $2
		order by i.created_at asc
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExportRow
	for rows.Next() {
		var r ExportRow
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.Status, &r.UserID, &r.TGUserID, &r.Text, &r.Latitude, &r.Longitude); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) ListAllChatIDs(ctx context.Context) ([]int64, error) {
	rows, err := db.Pool.Query(ctx, `select chat_id from chats`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (db *DB) ListAttachmentsByIssue(ctx context.Context, issueID int64) ([]Attachment, error) {
	rows, err := db.Pool.Query(ctx, `
		select id, issue_id, file_id, file_type, local_path, created_at
		from attachments
		where issue_id = $1
		order by id
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(
			&a.ID,
			&a.IssueID,
			&a.FileID,
			&a.FileType,
			&a.LocalPath,
			&a.CreatedAt,
		); err != nil {
			return nil, err
		}
		res = append(res, a)
	}
	return res, rows.Err()
}

func (db *DB) ListIssuesByUserPage(ctx context.Context, userID int64, limit, offset int) ([]Issue, error) {
	rows, err := db.Pool.Query(ctx, `
		select id, user_id, chat_id, text, latitude, longitude, status, district, category, created_at, updated_at
		from issues
		where user_id = $1
		order by created_at desc
		limit $2 offset $3
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Issue
	for rows.Next() {
		var x Issue
		if err := rows.Scan(&x.ID, &x.UserID, &x.ChatID, &x.Text,
			&x.Latitude, &x.Longitude, &x.Status,
			&x.District, &x.Category,
			&x.CreatedAt, &x.UpdatedAt,
		); err != nil {
			return nil, err
		}
		res = append(res, x)
	}
	return res, rows.Err()
}

func (db *DB) ListIssuesByStatusPage(ctx context.Context, statuses []string, limit, offset int) ([]Issue, error) {
	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses)+2) // статусы + limit + offset

	for i, s := range statuses {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = s
	}
	limitPos := len(statuses) + 1
	offsetPos := len(statuses) + 2
	args[limitPos-1] = limit
	args[offsetPos-1] = offset

	q := fmt.Sprintf(`
		select id, user_id, chat_id, text, latitude, longitude, status, district, category, created_at, updated_at
		from issues
		where status in (%s)
		order by created_at desc
		limit $%d offset $%d
	`, strings.Join(placeholders, ","), limitPos, offsetPos)

	rows, err := db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Issue
	for rows.Next() {
		var x Issue
		if err := rows.Scan(
			&x.ID, &x.UserID, &x.ChatID, &x.Text,
			&x.Latitude, &x.Longitude, &x.Status,
			&x.District, &x.Category,
			&x.CreatedAt, &x.UpdatedAt,
		); err != nil {
			return nil, err
		}
		res = append(res, x)
	}
	return res, rows.Err()
}

func (db *DB) ListIssuesByStatusFilterPage(ctx context.Context, statuses []string, district, category *string, limit, offset int) ([]Issue, error) {
	placeholders := make([]string, len(statuses))
	args := make([]any, 0, len(statuses)+4)

	for i, s := range statuses {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, s)
	}

	where := fmt.Sprintf("status in (%s)", strings.Join(placeholders, ","))

	if district != nil && *district != "" {
		args = append(args, *district)
		where += fmt.Sprintf(" and district = $%d", len(args))
	}
	if category != nil && *category != "" {
		args = append(args, *category)
		where += fmt.Sprintf(" and category = $%d", len(args))
	}

	args = append(args, limit, offset)
	limitPos := len(args) - 1
	offsetPos := len(args)

	q := fmt.Sprintf(`
		select id, user_id, chat_id, text, latitude, longitude,
		       status, district, category, created_at, updated_at
		from issues
		where %s
		order by created_at desc
		limit $%d offset $%d
	`, where, limitPos, offsetPos)

	rows, err := db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Issue
	for rows.Next() {
		var x Issue
		if err := rows.Scan(
			&x.ID, &x.UserID, &x.ChatID, &x.Text,
			&x.Latitude, &x.Longitude, &x.Status,
			&x.District, &x.Category,
			&x.CreatedAt, &x.UpdatedAt,
		); err != nil {
			return nil, err
		}
		res = append(res, x)
	}
	return res, rows.Err()
}

func (db *DB) ListCommentsByIssue(ctx context.Context, issueID int64) ([]Comment, error) {
	rows, err := db.Pool.Query(ctx, `
		select id, issue_id, admin_user_id, text, created_at
		from comments
		where issue_id = $1
		order by created_at asc
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(
			&c.ID,
			&c.IssueID,
			&c.AdminUserID,
			&c.Text,
			&c.CreatedAt,
		); err != nil {
			return nil, err
		}
		res = append(res, c)
	}
	return res, rows.Err()
}

// AttachLocationToLastIssue привязывает геопозицию к последней заявке пользователя,
// у которой ещё нет координат и которая создана недавно (за последние 10 минут).
func (db *DB) AttachLocationToLastIssue(ctx context.Context, userID int64, lat, lon float64) (*Issue, error) {
	row := db.Pool.QueryRow(ctx, `
		update issues
		set latitude = $1,
		    longitude = $2,
		    updated_at = now()
		where id = (
			select id
			from issues
			where user_id = $3
			  and latitude is null
			  and longitude is null
			  and created_at > now() - interval '10 minutes'
			order by created_at desc
			limit 1
		)
		returning id, user_id, chat_id, text, latitude, longitude,
		          status, district, category, created_at, updated_at
	`, lat, lon, userID)

	var iss Issue
	if err := row.Scan(
		&iss.ID,
		&iss.UserID,
		&iss.ChatID,
		&iss.Text,
		&iss.Latitude,
		&iss.Longitude,
		&iss.Status,
		&iss.District,
		&iss.Category,
		&iss.CreatedAt,
		&iss.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &iss, nil
}
