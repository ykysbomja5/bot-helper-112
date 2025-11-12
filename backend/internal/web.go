package internal

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// структура HTTP-сервера (интерфейса и API)
type Web struct {
	Cfg      *Config
	DB       *DB
	Services *Services
	Bot      *Bot
}

func NewWeb(cfg *Config, db *DB, svc *Services, bot *Bot) *Web {
	return &Web{
		Cfg:      cfg,
		DB:       db,
		Services: svc,
		Bot:      bot,
	}
}

func (w *Web) StartHTTP(ctx context.Context) error {
	r := gin.Default()

	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Определяем пути до frontend и uploads
	_, b, _, _ := runtime.Caller(0)
	basePath := filepath.Join(filepath.Dir(b), "..")    // backend/
	frontendPath := filepath.Join(basePath, "frontend") // backend/frontend
	uploadsPath := filepath.Join(basePath, "uploads")   // backend/uploads

	// API

	// Создание новой заявки
	r.POST("/api/issues", func(c *gin.Context) {
		var req WebIssueRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Некорректные данные"})
			return
		}

		issue, err := w.Services.CreateWebIssue(c.Request.Context(), &req)
		if err != nil {
			c.JSON(500, gin.H{"error": "Ошибка при создании заявки"})
			return
		}

		c.JSON(200, gin.H{
			"message": "Заявка успешно создана",
			"id":      issue.ID,
			"status":  issue.Status,
		})
	})

	// Загрузка вложений к заявке
	r.POST("/api/issues/:id/attachments", func(c *gin.Context) {
		idStr := c.Param("id")
		issueID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || issueID <= 0 {
			c.JSON(400, gin.H{"error": "invalid issue id"})
			return
		}

		// читаем файлы из поля "attachments"
		form, err := c.MultipartForm()
		if err != nil {
			c.JSON(400, gin.H{"error": "failed to read multipart form"})
			return
		}
		files := form.File["attachments"]
		if len(files) == 0 {
			c.JSON(400, gin.H{"error": "no files"})
			return
		}

		if err := os.MkdirAll(uploadsPath, 0o755); err != nil {
			c.JSON(500, gin.H{"error": "failed to create upload dir"})
			return
		}

		var uploaded []gin.H

		for _, fh := range files {
			src, err := fh.Open()
			if err != nil {
				c.JSON(500, gin.H{"error": "failed to open uploaded file"})
				return
			}

			// делаем уникальное имя файла
			safeName := fmt.Sprintf("issue_%d_%d_%s", issueID, time.Now().Unix(), fh.Filename)

			// путь на диске
			dstPath := filepath.Join(uploadsPath, safeName)
			dst, err := os.Create(dstPath)
			if err != nil {
				src.Close()
				c.JSON(500, gin.H{"error": "failed to create file"})
				return
			}

			if _, err := io.Copy(dst, src); err != nil {
				src.Close()
				dst.Close()
				c.JSON(500, gin.H{"error": "failed to save file"})
				return
			}

			src.Close()
			dst.Close()

			fileType := fh.Header.Get("Content-Type")

			localPath := filepath.Join("uploads", safeName)

			if err := w.DB.AddWebAttachment(c, issueID, safeName, fileType, localPath); err != nil {
				c.JSON(500, gin.H{"error": "failed to save attachment meta"})
				return
			}

			urlPath := "/uploads/" + safeName

			uploaded = append(uploaded, gin.H{
				"name": safeName,
				"type": fileType,
				"url":  urlPath,
			})
		}

		c.JSON(200, gin.H{"uploaded": uploaded})
	})

	r.GET("/api/categories", func(c *gin.Context) {
		c.JSON(200, w.Services.GetCategories())
	})
	r.GET("/api/districts", func(c *gin.Context) {
		c.JSON(200, w.Services.GetDistricts())
	})

	// Админ

	r.GET("/admin/ping", func(c *gin.Context) {
		if !w.auth(c.Query("token")) {
			c.String(401, "unauthorized")
			return
		}
		c.String(200, "ok")
	})

	r.GET("/export", func(c *gin.Context) {
		if !w.auth(c.Query("token")) {
			c.String(401, "unauthorized")
			return
		}
		fromS := c.Query("from")
		toS := c.Query("to")
		from, err1 := time.Parse("2006-01-02", fromS)
		to, err2 := time.Parse("2006-01-02", toS)
		if err1 != nil || err2 != nil {
			c.String(400, "bad period")
			return
		}
		to = to.Add(24 * time.Hour)
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=export_%s..%s.csv", fromS, toS))
		c.Header("Content-Type", "text/csv; charset=utf-8")
		if err := w.Services.ExportCSV(c, from, to, c.Writer); err != nil {
			c.String(500, err.Error())
			return
		}
	})

	r.GET("/admin/issues", func(c *gin.Context) {
		if !w.auth(c.Query("token")) {
			c.String(401, "unauthorized")
			return
		}
		status := c.Query("status")
		statuses := []string{"Новая", "В обработке", "Завершено", "Отклонено"}
		if status != "" {
			statuses = []string{status}
		}
		items, err := w.DB.ListIssuesByStatus(c, statuses, 100)
		if err != nil {
			c.String(500, err.Error())
			return
		}
		c.JSON(200, items)
	})

	r.POST("/admin/status", func(c *gin.Context) {
		var req struct {
			Token   string  `json:"token"`
			IssueID int64   `json:"issue_id"`
			Status  string  `json:"status"`
			Comment *string `json:"comment"`
			AdminTG *int64  `json:"admin_tg"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.String(400, err.Error())
			return
		}
		if !w.auth(req.Token) {
			c.String(401, "unauthorized")
			return
		}
		if err := w.DB.SetIssueStatus(c, req.IssueID, req.Status, req.AdminTG, req.Comment); err != nil {
			c.String(500, err.Error())
			return
		}
		c.String(200, "ok")
	})

	r.POST("/admin/comment", func(c *gin.Context) {
		var req struct {
			Token   string `json:"token"`
			IssueID int64  `json:"issue_id"`
			Text    string `json:"text"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.String(400, err.Error())
			return
		}
		if !w.auth(req.Token) {
			c.String(401, "unauthorized")
			return
		}
		if req.IssueID <= 0 || req.Text == "" {
			c.String(400, "bad request")
			return
		}
		var adminTG int64
		rowAdmin := w.DB.Pool.QueryRow(c.Request.Context(), `select tg_user_id from users where is_admin = true order by id limit 1`)
		_ = rowAdmin.Scan(&adminTG)
		if adminTG != 0 {
			_ = w.DB.AddComment(c.Request.Context(), req.IssueID, adminTG, req.Text)
		}

		var chatID int64
		rowUser := w.DB.Pool.QueryRow(c.Request.Context(), `select chat_id from issues where id = $1`, req.IssueID)
		if err := rowUser.Scan(&chatID); err == nil && chatID != 0 && w.Bot != nil && w.Bot.API != nil {
			msgText := fmt.Sprintf("Комментарий по вашей заявке #%d:\n\n%s", req.IssueID, req.Text)
			_, _ = w.Bot.API.Send(tgbotapi.NewMessage(chatID, msgText))
		}

		c.String(200, "ok")
	})

	r.GET("/admin/issues/:id/attachments", func(c *gin.Context) {
		if !w.auth(c.Query("token")) {
			c.String(401, "unauthorized")
			return
		}
		idStr := c.Param("id")
		issueID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || issueID <= 0 {
			c.String(400, "bad issue id")
			return
		}
		atts, err := w.DB.GetAttachmentsByIssueID(c, issueID)
		if err != nil {
			c.String(500, err.Error())
			return
		}
		c.JSON(200, atts)
	})

	// Webhook

	if w.Cfg.UseWebhook {
		r.POST(w.Cfg.WebhookPath, func(c *gin.Context) {
			var update tgbotapi.Update
			if err := c.BindJSON(&update); err != nil {
				c.String(400, err.Error())
				return
			}
			w.Bot.HandleWebhookUpdate(c, update)
			c.Status(200)
		})
		log.Printf("📡 Webhook включен: %s", w.Cfg.WebhookPath)
	}

	// Health check
	r.GET("/healthz", func(c *gin.Context) {
		c.String(200, "ok")
	})

	// Статика
	r.Static("/static", frontendPath)
	r.Static("/uploads", uploadsPath)

	// Страница веб‑админки
	r.GET("/admin", func(c *gin.Context) {
		c.File(filepath.Join(frontendPath, "admin.html"))
	})

	// остальные пути
	r.NoRoute(func(c *gin.Context) {
		c.File(filepath.Join(frontendPath, "index.html"))
	})

	addr := ":" + w.Cfg.Port
	log.Printf("🌐 HTTP сервер запущен на http://localhost%s", addr)
	return r.Run(addr)
}

// Проверка токена администратора
func (w *Web) auth(token string) bool {
	if token == "" {
		return false
	}
	if w.Cfg.APIToken != "" && token == w.Cfg.APIToken {
		return true
	}
	if w.Cfg.AdminSecret != "" && token == w.Cfg.AdminSecret {
		return true
	}
	return false
}
