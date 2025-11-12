package internal

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"time"
)

type Services struct {
	DB *DB
}

func NewServices(db *DB) *Services {
	return &Services{DB: db}
}

func (s *Services) CreateWebIssue(ctx context.Context, req *WebIssueRequest) (*Issue, error) {
	log.Printf("Получен запрос из веб-формы: %s (%s, %s)", req.Name, req.District, req.Category)

	issue, err := s.DB.CreateWebIssue(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ошибка при создании заявки: %w", err)
	}

	return issue, nil
}

func (s *Services) AddWebAttachments(ctx context.Context, issueID int64, attachments []WebAttachment) error {
	for _, a := range attachments {
		if err := s.DB.AddWebAttachment(ctx, issueID, a.FileName, a.FileType, a.FileURL); err != nil {
			return fmt.Errorf("ошибка при добавлении вложения %s: %w", a.FileName, err)
		}
	}
	return nil
}

func (s *Services) GetWebIssue(ctx context.Context, issueID int64) (*Issue, []Attachment, error) {
	issue, err := s.DB.GetWebIssueByID(ctx, issueID)
	if err != nil {
		return nil, nil, err
	}

	attachments, err := s.DB.GetAttachmentsByIssueID(ctx, issueID)
	if err != nil {
		return issue, nil, err
	}

	return issue, attachments, nil
}

func (s *Services) ExportCSV(ctx context.Context, from, to time.Time, w io.Writer) error {
	rows, err := s.DB.ExportIssues(ctx, from, to)
	if err != nil {
		return fmt.Errorf("ошибка при экспорте: %w", err)
	}

	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{
		"id", "created_at", "status", "user_id", "tg_user_id", "text", "latitude", "longitude",
	})

	for _, r := range rows {
		lat, lon := "", ""
		if r.Latitude != nil {
			lat = fmt.Sprintf("%f", *r.Latitude)
		}
		if r.Longitude != nil {
			lon = fmt.Sprintf("%f", *r.Longitude)
		}

		_ = cw.Write([]string{
			fmt.Sprintf("%d", r.ID),
			r.CreatedAt.Format(time.RFC3339),
			r.Status,
			fmt.Sprintf("%d", r.UserID),
			fmt.Sprintf("%d", r.TGUserID),
			r.Text,
			lat,
			lon,
		})
	}

	log.Printf("CSV экспорт выполнен: %d строк", len(rows))
	return nil
}

func (s *Services) GetCategories() []string {
	return []string{
		"ЖКХ",
		"Дороги",
		"Освещение",
		"Транспорт",
		"Благоустройство",
		"Другое",
	}
}

func (s *Services) GetDistricts() []string {
	return []string{
		"Центральный",
		"Северный",
		"Южный",
		"Восточный",
		"Западный",
	}
}
