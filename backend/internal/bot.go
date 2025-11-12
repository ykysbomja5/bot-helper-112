package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	time "time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const issuesPageSize = 10

var districts = []string{
	"Каменнобродский",
	"Жовтневый",
	"Артемовский",
	"Ленинский",
}

var greetings = []string{
	"Здравствуйте! Я помощник Фоксик. Расскажите, какая у вас проблема?",
	"Приветствую! Опишите вашу ситуацию — я зафиксирую обращение.",
	"Добрый день! Готов принять ваше сообщение о проблеме.",
	"Фоксик на связи! Чем могу помочь?",
	"Здравствуйте! Опишите проблему, и я передам информацию ответственным.",
}

var issueAccess = []string{
	"Ваше обращение зарегистрировано. Номер заявки: ",
	"Спасибо за сообщение! Заявка принята в работу, ее номер: ",
	"Информация получена. Мы уже занимаемся вашим вопросом. Номер вашей заявки: ",
	"Заявка зафиксирована. Скоро с вами свяжутся. Номер вашей заявки: ",
	"Принято! Мы получили ваше сообщение и передадим его специалистам. Ваша заявка под номером: ",
}

var categories = []string{
	"ЖКХ",
	"Дороги и транспорт",
	"Благоустройство и экология",
	"Образование и культура",
	"Безопасность и правопорядок",
	"Связь и цифровые услуги",
}

type issueWizardState struct {
	District string
	Category string
}

type Bot struct {
	API              *tgbotapi.BotAPI
	Cfg              *Config
	DB               *DB
	Services         *Services
	pendingComments  map[int64]int64  // adminTGUserID -> issueID
	pendingBroadcast map[int64]string // adminTGUserID -> broadcast

	myPage             map[int64]int    // chatID -> текущая страница /my
	issuesPage         map[int64]int    // chatID -> текущая страница /issues
	lastMode           map[int64]string // chatID -> "my" или "issues"
	lastMyMessages     map[int64][]int  // какие сообщения удалить при смене страницы /my
	lastIssuesMessages map[int64][]int  // какие сообщения удалить при смене страницы /issues
	wizard             map[int64]*issueWizardState
	issuesFilter       map[int64]*issuesFilterState
}

type issuesFilterState struct {
	District string
	Category string
}

func NewBot(api *tgbotapi.BotAPI, db *DB, cfg *Config, svc *Services) *Bot {
	return &Bot{
		API:                api,
		DB:                 db,
		Cfg:                cfg,
		pendingComments:    make(map[int64]int64),
		Services:           svc,
		pendingBroadcast:   map[int64]string{},
		myPage:             make(map[int64]int),
		issuesPage:         make(map[int64]int),
		lastMode:           make(map[int64]string),
		lastMyMessages:     make(map[int64][]int),
		lastIssuesMessages: make(map[int64][]int),
		wizard:             make(map[int64]*issueWizardState),
		issuesFilter:       make(map[int64]*issuesFilterState),
	}
}

func (b *Bot) StartLongPolling(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.API.GetUpdatesChan(u)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case upd := <-updates:
			if upd.UpdateID == 0 && upd.Message == nil && upd.CallbackQuery == nil {
				continue
			}
			b.handleUpdate(ctx, upd)
		}
	}
}

func (b *Bot) HandleWebhookUpdate(ctx context.Context, upd tgbotapi.Update) {
	b.handleUpdate(ctx, upd)
}

func (b *Bot) handleUpdate(ctx context.Context, upd tgbotapi.Update) {
	if upd.Message != nil {
		b.handleMessage(ctx, upd.Message)
		return
	}
	if upd.CallbackQuery != nil {
		b.handleCallback(ctx, upd.CallbackQuery)
		return
	}
}

func (b *Bot) ensureUserAndChat(ctx context.Context, m *tgbotapi.Message) (*User, error) {
	u := &User{
		TGUserID:  m.From.ID,
		Username:  strPtrEmptyToNil(m.From.UserName),
		FirstName: strPtrEmptyToNil(m.From.FirstName),
		LastName:  strPtrEmptyToNil(m.From.LastName),
	}
	uu, err := b.DB.UpsertUser(ctx, u)
	if err != nil {
		return nil, err
	}
	c := &Chat{ChatID: m.Chat.ID, Type: m.Chat.Type, Title: strPtrEmptyToNil(m.Chat.Title)}
	if err := b.DB.UpsertChat(ctx, c); err != nil {
		return nil, err
	}
	return uu, nil
}

func (b *Bot) handleMessage(ctx context.Context, m *tgbotapi.Message) {
	var Stickers = []tgbotapi.StickerConfig{
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYJpEhbIbD_d64psAAF5Zt_g2RyMhdQAAt6HAAIUl5BIbp1fLFzsY602BA")), //приветствие 1					0
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pZZpEhibAgJq2QfceJaqZMOPpx9b0wACk44AAnrykEiVUhJPK41jXzYE")),   //приветствие 2					1
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYRpEhbNzAwED6zmvFAXkX8WcgL-igAC0YoAAmUZmUgzrMIUcF4qezYE")),   //думает 1						2
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pcJpEh013mPppDB0ppAF4YX2Vx2IIQACQosAAr7dkEit25esTahTTzYE")),   //думает 2 (пишет)				3
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYZpEhbPWW3E3jp6TTL6pp6s5-G7tQAChoYAAnvvkEi3onmYZF_JkjYE")),   //довольный 1					4
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9paBpEhn1OoOw8Z1L8GEI6p-Fy4x0MwACTYYAAjtWkEgewcHahF3n0zYE")),   //довольный 2 (с лапками)		5
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYppEhbSWw5iMNKfN7WJIV1UMK6togAC14kAAs7FkUg0EL0UntPucTYE")),   //злюк							6
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYhpEhbQ3L2W0p1fXqokxstKTq3mMgACDJMAAh-6kUh-cwTnECWSGzYE")),   //плачет							7
	}

	u, err := b.ensureUserAndChat(ctx, m)
	if err != nil {
		log.Printf("ensure user/chat: %v", err)
	}

	if m.Chat.IsGroup() || m.Chat.IsSuperGroup() || m.Chat.IsChannel() {
		if m.IsCommand() {
			b.handleCommand(ctx, m)
		}
		return
	}

	if m.IsCommand() {
		b.handleCommand(ctx, m)
		return
	}

	txt := strings.TrimSpace(m.Text)

	//1. Главное меню и пагинация

	switch txt {
	case "Мои обращения":
		b.sendMyIssuesPage(ctx, m.Chat.ID, m.From.ID, 1)
		return

	case "FAQ / Помощь":
		b.reply(m.Chat.ID, "Справка: отправьте текст проблемы, по желанию фото/видео и геолокацию.\n/my — мои обращения.\n/issues — просмотр активных заявок (для админов).")
		return

	case "⬅ Предыдущая":
		switch b.lastMode[m.Chat.ID] {
		case "my":
			page := b.myPage[m.Chat.ID]
			if page > 1 {
				page--
			}
			b.sendMyIssuesPage(ctx, m.Chat.ID, m.From.ID, page)
		case "issues":
			if ok, _ := b.DB.IsAdmin(ctx, m.From.ID); !ok {
				b.reply(m.Chat.ID, "Недостаточно прав")
				n := rand.Intn(2)
				b.API.Send(Stickers[n+5])
				return
			}
			page := b.issuesPage[m.Chat.ID]
			if page > 1 {
				page--
			}
			b.sendIssuesPage(ctx, m.Chat.ID, page)
		}
		return

	case "Следующая ➡":
		switch b.lastMode[m.Chat.ID] {
		case "my":
			page := b.myPage[m.Chat.ID]
			page++
			b.sendMyIssuesPage(ctx, m.Chat.ID, m.From.ID, page)
		case "issues":
			if ok, _ := b.DB.IsAdmin(ctx, m.From.ID); !ok {
				b.reply(m.Chat.ID, "Недостаточно прав")
				n := rand.Intn(2)
				b.API.Send(Stickers[n+5])
				return
			}
			page := b.issuesPage[m.Chat.ID]
			page++
			b.sendIssuesPage(ctx, m.Chat.ID, page)
		}
		return
	}

	//1.5. Сообщение только с геопозицией привязываем к последней заявке
	if m.Location != nil && !hasIssueContent(m) {
		if u != nil {
			lat := m.Location.Latitude
			lon := m.Location.Longitude

			iss, err := b.DB.AttachLocationToLastIssue(ctx, u.ID, lat, lon)
			if err != nil {
				b.reply(m.Chat.ID, "Не удалось привязать геопозицию к обращению: "+err.Error())
				n := rand.Intn(2)
				b.API.Send(Stickers[n+5])
				return
			}
			if iss == nil {
				b.reply(m.Chat.ID, "Не нашёл недавнее обращение без координат. Сначала отправьте текст с описанием проблемы, потом геопозицию.")
				n := rand.Intn(2)
				b.API.Send(Stickers[n+5])
				return
			}

			b.reply(m.Chat.ID, fmt.Sprintf("Геопозиция добавлена к заявке #%d", iss.ID))
			return
		}
		b.reply(m.Chat.ID, "Сначала отправьте текст с описанием проблемы, затем геопозицию.")
		return
	}

	//2. Выбор РАЙОНА

	for _, d := range districts {
		if txt == d {
			st := b.wizard[m.From.ID]
			if st == nil {
				st = &issueWizardState{}
				b.wizard[m.From.ID] = st
			}
			st.District = d
			st.Category = ""

			msg := tgbotapi.NewMessage(m.Chat.ID,
				fmt.Sprintf("Район: %s\nТеперь выберите категорию проблемы.", d),
			)
			msg.ReplyMarkup = makeCategoryKeyboard()
			b.API.Send(msg)
			return
		}
	}

	//3. Выбор КАТЕГОРИИ

	for _, c := range categories {
		if txt == c {
			st := b.wizard[m.From.ID]
			if st == nil || st.District == "" {
				b.reply(m.Chat.ID, "Сначала выберите район командой /add или /start.")
				return
			}
			st.Category = c

			msg := tgbotapi.NewMessage(
				m.Chat.ID,
				fmt.Sprintf(
					"Район: %s\nКатегория: %s\n\nТеперь опишите проблему текстом, "+
						"при необходимости приложите фото/видео и отправьте геопозицию.",
					st.District, st.Category,
				),
			)
			b.API.Send(msg)
			return
		}
	}

	//4. Режим комментария для админа
	if issueID, ok := b.pendingComments[m.From.ID]; ok {
		if isAdmin, _ := b.DB.IsAdmin(ctx, m.From.ID); isAdmin {
			if err := b.DB.AddComment(ctx, issueID, m.From.ID, m.Text); err != nil {
				b.reply(m.Chat.ID, "Не удалось сохранить комментарий: "+err.Error())
				return
			}
			b.reply(m.Chat.ID, fmt.Sprintf("Комментарий добавлен к заявке #%d", issueID))
			delete(b.pendingComments, m.From.ID)

			return
		}
	}

	//5. Завершение мастера создания заявки
	if st, ok := b.wizard[m.From.ID]; ok && st.District != "" && st.Category != "" {
		b.createIssueFromMessageWithMeta(ctx, m, st.District, st.Category)
		delete(b.wizard, m.From.ID)
		return
	}

	//6. Обычное создание заявки (без мастера)
	b.createIssueFromMessage(ctx, m)
}

// createIssueFromMessageWithMeta создает заявку с учётом района и категории
// и полностью повторяет обработку вложений, как в createIssueFromMessage
func (b *Bot) createIssueFromMessageWithMeta(ctx context.Context, m *tgbotapi.Message, district, category string) {
	var Stickers = []tgbotapi.StickerConfig{
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYJpEhbIbD_d64psAAF5Zt_g2RyMhdQAAt6HAAIUl5BIbp1fLFzsY602BA")), //приветствие 1					0
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pZZpEhibAgJq2QfceJaqZMOPpx9b0wACk44AAnrykEiVUhJPK41jXzYE")),   //приветствие 2					1
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYRpEhbNzAwED6zmvFAXkX8WcgL-igAC0YoAAmUZmUgzrMIUcF4qezYE")),   //думает 1						2
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pcJpEh013mPppDB0ppAF4YX2Vx2IIQACQosAAr7dkEit25esTahTTzYE")),   //думает 2 (пишет)				3
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYZpEhbPWW3E3jp6TTL6pp6s5-G7tQAChoYAAnvvkEi3onmYZF_JkjYE")),   //довольный 1					4
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9paBpEhn1OoOw8Z1L8GEI6p-Fy4x0MwACTYYAAjtWkEgewcHahF3n0zYE")),   //довольный 2 (с лапками)		5
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYppEhbSWw5iMNKfN7WJIV1UMK6togAC14kAAs7FkUg0EL0UntPucTYE")),   //злюк							6
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYhpEhbQ3L2W0p1fXqokxstKTq3mMgACDJMAAh-6kUh-cwTnECWSGzYE")),   //плачет							7
	}

	if !m.Chat.IsPrivate() {
		return
	}

	u, err := b.ensureUserAndChat(ctx, m)
	if err != nil {
		log.Printf("ensure: %v", err)
	}

	var text *string
	t := strings.TrimSpace(m.Text)
	if t == "" {
		t = strings.TrimSpace(m.Caption)
	}
	if t != "" {
		text = &t
	}

	var lat, lon *float64
	if m.Location != nil {
		lat = &m.Location.Latitude
		lon = &m.Location.Longitude
	}

	d := district
	c := category

	iss, err := b.DB.CreateIssue(ctx, &Issue{
		UserID:    u.ID,
		ChatID:    m.Chat.ID,
		Text:      text,
		Latitude:  lat,
		Longitude: lon,
		Status:    "Новая",
		District:  &d,
		Category:  &c,
	})
	if err != nil {
		b.reply(m.Chat.ID, "Не удалось создать заявку: "+err.Error())
		n := rand.Intn(2)
		b.API.Send(Stickers[n+5])
		return
	}

	//Вложения

	if len(m.Photo) > 0 {
		ph := m.Photo[len(m.Photo)-1]
		filename := fmt.Sprintf("issue_%d_photo_%d.jpg", iss.ID, time.Now().UnixNano())
		localPath, err := b.saveTelegramFile(ph.FileID, filename)
		if err != nil {
			log.Printf("save photo failed: %v", err)
		} else {
			_ = b.DB.AddAttachment(ctx, &Attachment{
				IssueID:   iss.ID,
				FileID:    ph.FileID,
				FileType:  "photo",
				LocalPath: localPath,
			})
		}
	}

	if m.Video != nil {
		ext := ".mp4"
		if m.Video.FileName != "" {
			if e := filepath.Ext(m.Video.FileName); e != "" {
				ext = e
			}
		}
		filename := fmt.Sprintf("issue_%d_video_%d%s", iss.ID, time.Now().UnixNano(), ext)
		localPath, err := b.saveTelegramFile(m.Video.FileID, filename)
		if err != nil {
			log.Printf("save video failed: %v", err)
		} else {
			_ = b.DB.AddAttachment(ctx, &Attachment{
				IssueID:   iss.ID,
				FileID:    m.Video.FileID,
				FileType:  "video",
				LocalPath: localPath,
			})
		}
	}

	if m.Document != nil {
		filename := m.Document.FileName
		if filename == "" {
			filename = fmt.Sprintf("issue_%d_doc_%d", iss.ID, time.Now().UnixNano())
		}
		localPath, err := b.saveTelegramFile(m.Document.FileID, filename)
		if err != nil {
			log.Printf("save document failed: %v", err)
		} else {
			_ = b.DB.AddAttachment(ctx, &Attachment{
				IssueID:   iss.ID,
				FileID:    m.Document.FileID,
				FileType:  "document",
				LocalPath: localPath,
			})
		}
	}

	n := rand.Intn(len(issueAccess) - 1)
	b.reply(m.Chat.ID, fmt.Sprintln(issueAccess[n], iss.ID))
	n = rand.Intn(2)
	b.API.Send(Stickers[n+4])
	// Периодическое уведомление админам
	if shouldExecuteQuarterly() {
		b.notifyAdminsNewIssue(ctx)
	}
}

func (b *Bot) deleteMessages(chatID int64, ids []int) {
	for _, id := range ids {
		_, _ = b.API.Request(tgbotapi.NewDeleteMessage(chatID, id))
	}
}

func (b *Bot) handleCommand(ctx context.Context, m *tgbotapi.Message) {
	var Stickers = []tgbotapi.StickerConfig{
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYJpEhbIbD_d64psAAF5Zt_g2RyMhdQAAt6HAAIUl5BIbp1fLFzsY602BA")), //приветствие 1					0
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pZZpEhibAgJq2QfceJaqZMOPpx9b0wACk44AAnrykEiVUhJPK41jXzYE")),   //приветствие 2					1
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYRpEhbNzAwED6zmvFAXkX8WcgL-igAC0YoAAmUZmUgzrMIUcF4qezYE")),   //думает 1						2
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pcJpEh013mPppDB0ppAF4YX2Vx2IIQACQosAAr7dkEit25esTahTTzYE")),   //думает 2 (пишет)				3
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYZpEhbPWW3E3jp6TTL6pp6s5-G7tQAChoYAAnvvkEi3onmYZF_JkjYE")),   //довольный 1					4
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9paBpEhn1OoOw8Z1L8GEI6p-Fy4x0MwACTYYAAjtWkEgewcHahF3n0zYE")),   //довольный 2 (с лапками)		5
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYppEhbSWw5iMNKfN7WJIV1UMK6togAC14kAAs7FkUg0EL0UntPucTYE")),   //злюк							6
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYhpEhbQ3L2W0p1fXqokxstKTq3mMgACDJMAAh-6kUh-cwTnECWSGzYE")),   //плачет							7
	}

	switch m.Command() {
	case "start":
		delete(b.wizard, m.From.ID)
		n := rand.Intn(6)
		text := greetings[n]
		msg := tgbotapi.NewMessage(m.Chat.ID, text)
		b.API.Send(msg)
		n = rand.Intn(2)
		b.API.Send(Stickers[n])
		text = "Для начала выберите район, в котором возникла проблема."
		msg = tgbotapi.NewMessage(m.Chat.ID, text)
		msg.ReplyMarkup = makeDistrictKeyboard()
		b.API.Send(msg)
		return
	case "help":
		b.reply(m.Chat.ID, "Справка: отправьте текст проблемы, фото/видео и геолокацию. В группах бот сообщения не обрабатывает. Для администраторов: /admin <секрет>, /export <период>, /broadcast \"текст\".")
	case "my":
		b.sendMyIssuesPage(ctx, m.Chat.ID, m.From.ID, 1)
	case "admin":
		args := strings.TrimSpace(m.CommandArguments())
		if args == "" {
			b.reply(m.Chat.ID, "Укажите секрет: /admin <секрет>")
			return
		}
		if args != b.Cfg.AdminSecret {
			b.reply(m.Chat.ID, "Неверный секрет")
			n := rand.Intn(2)
			b.API.Send(Stickers[n+5])
			return
		}
		if err := b.DB.PromoteToAdmin(ctx, m.From.ID); err != nil {
			b.reply(m.Chat.ID, "Не удалось выдать права: "+err.Error())
			n := rand.Intn(2)
			b.API.Send(Stickers[n+5])
			return
		}
		b.reply(m.Chat.ID, "Права администратора выданы. Доступны команды /export, /broadcast, /issues. Новые заявки будут приходить автоматически.")
		n := rand.Intn(2)
		b.API.Send(Stickers[n+4])
	case "export":
		period := strings.TrimSpace(m.CommandArguments())
		from, to, err := parsePeriod(period)
		if err != nil {
			b.reply(m.Chat.ID, "Формат: /export YYYY-MM-DD..YYYY-MM-DD")
			return
		}
		if ok, _ := b.DB.IsAdmin(ctx, m.From.ID); !ok {
			b.reply(m.Chat.ID, "Недостаточно прав")
			n := rand.Intn(2)
			b.API.Send(Stickers[n+6])
			return
		}
		var sb strings.Builder
		sb.WriteString("id,created_at,status,user_id,tg_user_id,text,latitude,longitude\n")
		if err := b.Services.ExportCSV(ctx, from, to, &sb); err != nil {
			b.reply(m.Chat.ID, "Ошибка экспорта: "+err.Error())
			return
		}
		b.reply(m.Chat.ID, "Экспорт за период: "+from.Format("2006-01-02")+".."+to.Add(-time.Nanosecond).Format("2006-01-02"))
		b.API.Send(tgbotapi.NewDocument(m.Chat.ID, tgbotapi.FileBytes{Name: "export.csv", Bytes: []byte(sb.String())}))
	case "broadcast":
		if ok, _ := b.DB.IsAdmin(ctx, m.From.ID); !ok {
			b.reply(m.Chat.ID, "Недостаточно прав")
			n := rand.Intn(2)
			b.API.Send(Stickers[n+6])
			return
		}
		text := strings.TrimSpace(m.CommandArguments())
		if text == "" {
			b.reply(m.Chat.ID, "Использование: /broadcast \"Текст\" — будет предпросмотр и подтверждение.")
			return
		}
		b.pendingBroadcast[m.From.ID] = text
		msg := tgbotapi.NewMessage(m.Chat.ID, "Предпросмотр рассылки:\n\n"+text)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "broadcast:confirm"),
				tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "broadcast:cancel"),
			),
		)
		msg.ReplyMarkup = kb
		b.API.Send(msg)
	case "issues":
		if ok, _ := b.DB.IsAdmin(ctx, m.From.ID); !ok {
			b.reply(m.Chat.ID, "Недостаточно прав")
			return
		}

		delete(b.issuesFilter, m.Chat.ID)

		b.sendIssuesPage(ctx, m.Chat.ID, 1)
		return
	case "issues_filter":
		if ok, _ := b.DB.IsAdmin(ctx, m.From.ID); !ok {
			b.reply(m.Chat.ID, "Недостаточно прав")
			return
		}
		b.sendIssuesFilterDistrictMenu(m.Chat.ID)
		return
	case "add":
		delete(b.wizard, m.From.ID)

		msg := tgbotapi.NewMessage(m.Chat.ID,
			"Создаём новое обращение.\nСначала выберите район, в котором возникла проблема.",
		)
		msg.ReplyMarkup = makeDistrictKeyboard()
		b.API.Send(msg)
		return
	default:
		if strings.EqualFold(strings.TrimSpace(m.Text), "Мои обращения") {
			b.sendMyIssuesPage(ctx, m.Chat.ID, m.From.ID, 1)
			return
		}

		if strings.Contains(strings.ToLower(m.Text), "faq") {
			b.handleCommand(ctx, &tgbotapi.Message{Chat: m.Chat, From: m.From, Text: "/help"})
			return
		}
	}
}

func makeUserPagingKeyboard() tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅ Предыдущая"),
			tgbotapi.NewKeyboardButton("Следующая ➡"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Мои обращения"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("FAQ / Помощь"),
		),
	)
	kb.ResizeKeyboard = true
	return kb
}

func parsePeriod(p string) (time.Time, time.Time, error) {
	parts := strings.Split(p, "..")
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, errors.New("bad period")
	}
	from, err := time.Parse("2006-01-02", strings.TrimSpace(parts[0]))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := time.Parse("2006-01-02", strings.TrimSpace(parts[1]))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return from, to.Add(24 * time.Hour), nil
}

// sendMyIssuesPage показывает страницу page (1,2,3,...) заявок пользователя.
func (b *Bot) sendMyIssuesPage(ctx context.Context, chatID int64, tgUserID int64, page int) {
	if page < 1 {
		page = 1
	}
	b.lastMode[chatID] = "my"
	b.myPage[chatID] = page

	if ids := b.lastMyMessages[chatID]; len(ids) > 0 {
		b.deleteMessages(chatID, ids)
		b.lastMyMessages[chatID] = nil
	}

	row := b.DB.Pool.QueryRow(ctx, `select id from users where tg_user_id=$1`, tgUserID)
	var uid int64
	if err := row.Scan(&uid); err != nil {
		msg := tgbotapi.NewMessage(chatID, "Нет обращений")
		msg.ReplyMarkup = makeUserPagingKeyboard()
		sent, _ := b.API.Send(msg)
		b.lastMyMessages[chatID] = append(b.lastMyMessages[chatID], sent.MessageID)
		return
	}

	offset := (page - 1) * issuesPageSize
	issues, err := b.DB.ListIssuesByUserPage(ctx, uid, issuesPageSize, offset)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "Ошибка загрузки обращений: "+err.Error())
		msg.ReplyMarkup = makeUserPagingKeyboard()
		sent, _ := b.API.Send(msg)
		b.lastMyMessages[chatID] = append(b.lastMyMessages[chatID], sent.MessageID)
		return
	}
	if len(issues) == 0 {
		text := "Пока нет обращений"
		if page > 1 {
			text = "На этой странице обращений нет."
		}
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ReplyMarkup = makeUserPagingKeyboard()
		sent, _ := b.API.Send(msg)
		b.lastMyMessages[chatID] = append(b.lastMyMessages[chatID], sent.MessageID)
		return
	}

	header := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ваши обращения (страница %d):", page))
	header.ReplyMarkup = makeUserPagingKeyboard()
	sentHeader, _ := b.API.Send(header)
	b.lastMyMessages[chatID] = append(b.lastMyMessages[chatID], sentHeader.MessageID)

	for _, is := range issues {
		text := "(без текста)"
		if is.Text != nil && *is.Text != "" {
			text = *is.Text
		}

		extra := ""
		if is.District != nil && *is.District != "" {
			extra += "\nРайон: " + *is.District
		}
		if is.Category != nil && *is.Category != "" {
			extra += "\nКатегория: " + *is.Category
		}

		var lastCommentText string
		if comments, err := b.DB.ListCommentsByIssue(ctx, is.ID); err == nil && len(comments) > 0 {
			last := comments[len(comments)-1]
			lastCommentText = last.Text
		}

		caption := fmt.Sprintf(
			"#%d — %s%s\n%s",
			is.ID,
			is.Status,
			extra,
			trim(text, 200),
		)
		if lastCommentText != "" {
			caption += "\n\nКомментарий администрации:\n" + lastCommentText
		}

		atts, _ := b.DB.ListAttachmentsByIssue(ctx, is.ID)

		var mainPhoto *Attachment
		var rest []Attachment
		for _, a := range atts {
			if mainPhoto == nil && a.FileType == "photo" {
				cp := a
				mainPhoto = &cp
			} else {
				rest = append(rest, a)
			}
		}

		if mainPhoto != nil {
			var msg tgbotapi.Message
			if mainPhoto.LocalPath != "" {
				photo := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(mainPhoto.LocalPath))
				photo.Caption = caption
				msg, _ = b.API.Send(photo)
			} else {
				photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileID(mainPhoto.FileID))
				photo.Caption = caption
				msg, _ = b.API.Send(photo)
			}
			if msg.MessageID != 0 {
				b.lastMyMessages[chatID] = append(b.lastMyMessages[chatID], msg.MessageID)
			}

			if len(rest) > 0 {
				ids := b.sendAttachmentsList(chatID, rest)
				if len(ids) > 0 {
					b.lastMyMessages[chatID] = append(b.lastMyMessages[chatID], ids...)
				}
			}
		} else {
			msg := tgbotapi.NewMessage(chatID, caption)
			sent, _ := b.API.Send(msg)
			if sent.MessageID != 0 {
				b.lastMyMessages[chatID] = append(b.lastMyMessages[chatID], sent.MessageID)
			}

			ids := b.sendIssueAttachments(ctx, chatID, is.ID)
			if len(ids) > 0 {
				b.lastMyMessages[chatID] = append(b.lastMyMessages[chatID], ids...)
			}
		}
	}
}

// sendIssuesPage показывает администратору страницу page заявок со статусами Новая/В обработке.
func (b *Bot) sendIssuesPage(ctx context.Context, chatID int64, page int) {
	if page < 1 {
		page = 1
	}
	b.lastMode[chatID] = "issues"
	b.issuesPage[chatID] = page

	if ids := b.lastIssuesMessages[chatID]; len(ids) > 0 {
		b.deleteMessages(chatID, ids)
		b.lastIssuesMessages[chatID] = nil
	}

	var districtPtr, categoryPtr *string
	filter, hasFilter := b.issuesFilter[chatID]
	if hasFilter {
		if filter.District != "" {
			d := filter.District
			districtPtr = &d
		}
		if filter.Category != "" {
			c := filter.Category
			categoryPtr = &c
		}
	}

	statuses := []string{"Новая", "В обработке"}
	offset := (page - 1) * issuesPageSize

	list, err := b.DB.ListIssuesByStatusFilterPage(ctx, statuses, districtPtr, categoryPtr, issuesPageSize, offset)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "Ошибка загрузки заявок: "+err.Error())
		msg.ReplyMarkup = makeAdminPagingKeyboard()
		sent, _ := b.API.Send(msg)
		b.lastIssuesMessages[chatID] = append(b.lastIssuesMessages[chatID], sent.MessageID)
		return
	}
	if len(list) == 0 {
		text := "Нет новых или активных заявок."
		if page > 1 {
			text = "На этой странице заявок нет."
		}
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ReplyMarkup = makeAdminPagingKeyboard()
		sent, _ := b.API.Send(msg)
		b.lastIssuesMessages[chatID] = append(b.lastIssuesMessages[chatID], sent.MessageID)
		return
	}

	headerText := fmt.Sprintf("Заявки (страница %d)", page)
	if hasFilter && (filter.District != "" || filter.Category != "") {
		headerText += "\nФильтр:"
		if filter.District != "" {
			headerText += " район — " + filter.District
		}
		if filter.Category != "" {
			if filter.District != "" {
				headerText += ","
			}
			headerText += " категория — " + filter.Category
		}
	}

	header := tgbotapi.NewMessage(chatID, headerText)
	header.ReplyMarkup = makeAdminPagingKeyboard()
	sentHeader, _ := b.API.Send(header)
	b.lastIssuesMessages[chatID] = append(b.lastIssuesMessages[chatID], sentHeader.MessageID)

	for i := range list {
		iss := list[i]
		ids := b.sendIssueToChat(ctx, chatID, &iss)
		if len(ids) > 0 {
			b.lastIssuesMessages[chatID] = append(b.lastIssuesMessages[chatID], ids...)
		}
	}
}

// sendIssueAttachments отправляет вложения заявки и возвращает id созданных сообщений.
func (b *Bot) sendIssueAttachments(ctx context.Context, chatID int64, issueID int64) []int {
	atts, err := b.DB.ListAttachmentsByIssue(ctx, issueID)
	if err != nil || len(atts) == 0 {
		return nil
	}
	return b.sendAttachmentsList(chatID, atts)
}

// sendAttachmentsList отправляет вложения и возвращает id созданных сообщений.
func (b *Bot) sendAttachmentsList(chatID int64, atts []Attachment) []int {
	var ids []int

	for _, a := range atts {
		usePath := a.LocalPath != ""
		var msg tgbotapi.Message

		switch a.FileType {
		case "photo":
			if usePath {
				msg, _ = b.API.Send(tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(a.LocalPath)))
			} else {
				msg, _ = b.API.Send(tgbotapi.NewPhoto(chatID, tgbotapi.FileID(a.FileID)))
			}
		case "video":
			if usePath {
				msg, _ = b.API.Send(tgbotapi.NewVideo(chatID, tgbotapi.FilePath(a.LocalPath)))
			} else {
				msg, _ = b.API.Send(tgbotapi.NewVideo(chatID, tgbotapi.FileID(a.FileID)))
			}
		default:
			if usePath {
				msg, _ = b.API.Send(tgbotapi.NewDocument(chatID, tgbotapi.FilePath(a.LocalPath)))
			} else {
				msg, _ = b.API.Send(tgbotapi.NewDocument(chatID, tgbotapi.FileID(a.FileID)))
			}
		}

		if msg.MessageID != 0 {
			ids = append(ids, msg.MessageID)
		}
	}

	return ids
}

func (b *Bot) createIssueFromMessage(ctx context.Context, m *tgbotapi.Message) {
	var Stickers = []tgbotapi.StickerConfig{
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYJpEhbIbD_d64psAAF5Zt_g2RyMhdQAAt6HAAIUl5BIbp1fLFzsY602BA")), //приветствие 1					0
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pZZpEhibAgJq2QfceJaqZMOPpx9b0wACk44AAnrykEiVUhJPK41jXzYE")),   //приветствие 2					1
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYRpEhbNzAwED6zmvFAXkX8WcgL-igAC0YoAAmUZmUgzrMIUcF4qezYE")),   //думает 1						2
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pcJpEh013mPppDB0ppAF4YX2Vx2IIQACQosAAr7dkEit25esTahTTzYE")),   //думает 2 (пишет)				3
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYZpEhbPWW3E3jp6TTL6pp6s5-G7tQAChoYAAnvvkEi3onmYZF_JkjYE")),   //довольный 1					4
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9paBpEhn1OoOw8Z1L8GEI6p-Fy4x0MwACTYYAAjtWkEgewcHahF3n0zYE")),   //довольный 2 (с лапками)		5
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYppEhbSWw5iMNKfN7WJIV1UMK6togAC14kAAs7FkUg0EL0UntPucTYE")),   //злюк							6
		tgbotapi.NewSticker(m.Chat.ID, tgbotapi.FileID("CAACAgIAAxkBAAE9pYhpEhbQ3L2W0p1fXqokxstKTq3mMgACDJMAAh-6kUh-cwTnECWSGzYE")),   //плачет							7
	}
	if !m.Chat.IsPrivate() {
		return
	}

	u, err := b.ensureUserAndChat(ctx, m)
	if err != nil {
		log.Printf("ensure: %v", err)
	}

	var text *string
	t := strings.TrimSpace(m.Text)
	if t == "" {
		t = strings.TrimSpace(m.Caption)
	}
	if t != "" {
		text = &t
	}

	var lat, lon *float64
	if m.Location != nil {
		lat = &m.Location.Latitude
		lon = &m.Location.Longitude
	}

	iss, err := b.DB.CreateIssue(ctx, &Issue{
		UserID:    u.ID,
		ChatID:    m.Chat.ID,
		Text:      text,
		Latitude:  lat,
		Longitude: lon,
	})
	if err != nil {
		b.reply(m.Chat.ID, "Не удалось создать заявку: "+err.Error())
		return
	}

	if len(m.Photo) > 0 {
		ph := m.Photo[len(m.Photo)-1]
		filename := fmt.Sprintf("issue_%d_photo_%d.jpg", iss.ID, time.Now().UnixNano())
		localPath, err := b.saveTelegramFile(ph.FileID, filename)
		if err != nil {
			log.Printf("save photo failed: %v", err)
		} else {
			_ = b.DB.AddAttachment(ctx, &Attachment{
				IssueID:   iss.ID,
				FileID:    ph.FileID,
				FileType:  "photo",
				LocalPath: localPath,
			})
		}
	}

	if m.Video != nil {
		ext := ".mp4"
		if m.Video.FileName != "" {
			if e := filepath.Ext(m.Video.FileName); e != "" {
				ext = e
			}
		}
		filename := fmt.Sprintf("issue_%d_video_%d%s", iss.ID, time.Now().UnixNano(), ext)
		localPath, err := b.saveTelegramFile(m.Video.FileID, filename)
		if err != nil {
			log.Printf("save video failed: %v", err)
		} else {
			_ = b.DB.AddAttachment(ctx, &Attachment{
				IssueID:   iss.ID,
				FileID:    m.Video.FileID,
				FileType:  "video",
				LocalPath: localPath,
			})
		}
	}

	if m.Document != nil {
		filename := m.Document.FileName
		if filename == "" {
			filename = fmt.Sprintf("issue_%d_doc_%d", iss.ID, time.Now().UnixNano())
		}
		localPath, err := b.saveTelegramFile(m.Document.FileID, filename)
		if err != nil {
			log.Printf("save document failed: %v", err)
		} else {
			_ = b.DB.AddAttachment(ctx, &Attachment{
				IssueID:   iss.ID,
				FileID:    m.Document.FileID,
				FileType:  "document",
				LocalPath: localPath,
			})
		}
	}

	b.reply(m.Chat.ID, fmt.Sprintf("Заявка принята, номер %d", iss.ID))
	n := rand.Intn(2)
	b.API.Send(Stickers[n+4])
	if shouldExecuteQuarterly() {
		b.notifyAdminsNewIssue(ctx)
	}
}

// GetIssueByID возвращает заявку по id.
func (db *DB) GetIssueByID(ctx context.Context, id int64) (*Issue, error) {
	row := db.Pool.QueryRow(ctx, `
		select id, user_id, chat_id, text, latitude, longitude,
		       status, district, category, created_at, updated_at
		from issues
		where id = $1
	`, id)

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
		return nil, err
	}
	return &iss, nil
}

var lastExecutionMinute = -1

func shouldExecuteQuarterly() bool {
	now := time.Now()
	minute := now.Minute()

	if (minute == 0 || minute == 15 || minute == 30 || minute == 45) &&
		minute != lastExecutionMinute {
		lastExecutionMinute = minute
		return true
	}
	if !(minute == 0 || minute == 15 || minute == 30 || minute == 45) {
		lastExecutionMinute = -1
	}

	return false
}

func (b *Bot) notifyAdminsNewIssue(ctx context.Context) {
	rows, err := b.DB.Pool.Query(ctx, `SELECT tg_user_id FROM users WHERE is_admin = true`)
	if err != nil {
		return
	}
	defer rows.Close()

	var totalNew int
	err = b.DB.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM issues WHERE status = 'Новая'`,
	).Scan(&totalNew)
	if err != nil {
		return
	}

	var recentNew int
	since := time.Now().Add(-15 * time.Minute)
	err = b.DB.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM issues WHERE status = 'Новая' AND created_at >= $1`,
		since,
	).Scan(&recentNew)
	if err != nil {
		return
	}

	text := fmt.Sprintf(
		"Общее количество заявок со статусом \"Новая\": %d\n"+
			"Количество новых заявок за последние 15 минут: %d",
		totalNew, recentNew,
	)

	for rows.Next() {
		var adminTG int64
		if err := rows.Scan(&adminTG); err != nil {
			continue
		}
		msg := tgbotapi.NewMessage(adminTG, text)
		b.API.Send(msg)
	}
}

// sendIssueToChat шлёт заявку (текст/фото/кнопки) и возвращает id всех сообщений.
func (b *Bot) sendIssueToChat(ctx context.Context, chatID int64, iss *Issue) []int {
	var ids []int

	textBody := "(без текста)"
	if iss.Text != nil && *iss.Text != "" {
		textBody = *iss.Text
	}

	extra := ""
	if iss.District != nil && *iss.District != "" {
		extra += "\nРайон: " + *iss.District
	}
	if iss.Category != nil && *iss.Category != "" {
		extra += "\nКатегория: " + *iss.Category
	}
	if iss.Latitude != nil && iss.Longitude != nil {
		extra += fmt.Sprintf("\nКоординаты: %.6f, %.6f", *iss.Latitude, *iss.Longitude)
	}

	var lastCommentText string
	if comments, err := b.DB.ListCommentsByIssue(ctx, iss.ID); err == nil && len(comments) > 0 {
		last := comments[len(comments)-1]
		lastCommentText = last.Text
	}

	caption := fmt.Sprintf(
		"Заявка #%d\nСтатус: %s%s\n%s",
		iss.ID,
		iss.Status,
		extra,
		trim(textBody, 200),
	)
	if lastCommentText != "" {
		caption += "\n\nКомментарий администратора:\n" + lastCommentText
	}

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("В обработке", fmt.Sprintf("status:%d:%s", iss.ID, "В обработке")),
			tgbotapi.NewInlineKeyboardButtonData("Завершено", fmt.Sprintf("status:%d:%s", iss.ID, "Завершено")),
			tgbotapi.NewInlineKeyboardButtonData("Отклонено", fmt.Sprintf("status:%d:%s", iss.ID, "Отклонено")),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💬 Комментарий", fmt.Sprintf("comment:%d", iss.ID)),
		),
	)

	atts, _ := b.DB.ListAttachmentsByIssue(ctx, iss.ID)

	var mainPhoto *Attachment
	var rest []Attachment
	for _, a := range atts {
		if mainPhoto == nil && a.FileType == "photo" {
			cp := a
			mainPhoto = &cp
		} else {
			rest = append(rest, a)
		}
	}

	if mainPhoto != nil {
		var msg tgbotapi.Message
		if mainPhoto.LocalPath != "" {
			photo := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(mainPhoto.LocalPath))
			photo.Caption = caption
			photo.ReplyMarkup = kb
			msg, _ = b.API.Send(photo)
		} else {
			photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileID(mainPhoto.FileID))
			photo.Caption = caption
			photo.ReplyMarkup = kb
			msg, _ = b.API.Send(photo)
		}
		if msg.MessageID != 0 {
			ids = append(ids, msg.MessageID)
		}

		if len(rest) > 0 {
			restIDs := b.sendAttachmentsList(chatID, rest)
			ids = append(ids, restIDs...)
		}
	} else {
		msg := tgbotapi.NewMessage(chatID, caption)
		msg.ReplyMarkup = kb
		sent, _ := b.API.Send(msg)
		if sent.MessageID != 0 {
			ids = append(ids, sent.MessageID)
		}

		restIDs := b.sendIssueAttachments(ctx, chatID, iss.ID)
		ids = append(ids, restIDs...)
	}

	return ids
}

func (b *Bot) handleCallback(ctx context.Context, cq *tgbotapi.CallbackQuery) {
	data := cq.Data

	if strings.HasPrefix(data, "my:page:") {
		parts := strings.Split(data, ":")
		if len(parts) != 3 {
			return
		}
		page, err := strconv.Atoi(parts[2])
		if err != nil || page < 1 {
			page = 1
		}
		chatID := cq.Message.Chat.ID
		b.sendMyIssuesPage(ctx, chatID, cq.From.ID, page)
		b.answerCallback(cq, fmt.Sprintf("Страница %d", page))
		return
	}
	if strings.HasPrefix(data, "issues:page:") {
		parts := strings.Split(data, ":")
		if len(parts) != 3 {
			return
		}
		page, err := strconv.Atoi(parts[2])
		if err != nil || page < 1 {
			page = 1
		}
		if ok, _ := b.DB.IsAdmin(ctx, cq.From.ID); !ok {
			b.answerCallback(cq, "Нет прав")
			return
		}
		chatID := cq.Message.Chat.ID
		b.sendIssuesPage(ctx, chatID, page)
		b.answerCallback(cq, fmt.Sprintf("Страница %d", page))
		return
	}

	if strings.HasPrefix(data, "if:d:") {
		if ok, _ := b.DB.IsAdmin(ctx, cq.From.ID); !ok {
			b.answerCallback(cq, "Нет прав")
			return
		}

		choice := strings.TrimPrefix(data, "if:d:")
		chatID := cq.Message.Chat.ID

		st := b.issuesFilter[chatID]
		if st == nil {
			st = &issuesFilterState{}
		}

		if choice == "ALL" {
			st.District = ""
		} else {
			st.District = choice
		}
		st.Category = ""
		b.issuesFilter[chatID] = st

		var rows [][]tgbotapi.InlineKeyboardButton
		for _, c := range categories {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(c, "if:c:"+c),
			))
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Все категории", "if:c:ALL"),
		))

		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		msg := tgbotapi.NewMessage(chatID, "Выберите категорию для фильтрации:")
		msg.ReplyMarkup = kb
		b.API.Send(msg)

		b.answerCallback(cq, "Район выбран")
		return
	}

	if strings.HasPrefix(data, "if:c:") {
		if ok, _ := b.DB.IsAdmin(ctx, cq.From.ID); !ok {
			b.answerCallback(cq, "Нет прав")
			return
		}

		choice := strings.TrimPrefix(data, "if:c:")
		chatID := cq.Message.Chat.ID

		st := b.issuesFilter[chatID]
		if st == nil {
			st = &issuesFilterState{}
		}

		if choice == "ALL" {
			st.Category = ""
		} else {
			st.Category = choice
		}
		b.issuesFilter[chatID] = st

		b.sendIssuesPage(ctx, chatID, 1)
		b.answerCallback(cq, "Фильтр применён")
		return
	}

	if strings.HasPrefix(data, "status:") {
		parts := strings.Split(data, ":")
		if len(parts) != 3 {
			return
		}
		issueID, _ := strconv.ParseInt(parts[1], 10, 64)
		newStatus := parts[2]
		if ok, _ := b.DB.IsAdmin(ctx, cq.From.ID); !ok {
			b.answerCallback(cq, "Нет прав")
			return
		}
		if err := b.DB.SetIssueStatus(ctx, issueID, newStatus, &cq.From.ID, nil); err != nil {
			log.Printf("SetIssueStatus error: %v", err)
			b.answerCallback(cq, "Ошибка статуса")
			return
		}
		b.answerCallback(cq, fmt.Sprintf("Статус #%d: %s", issueID, newStatus))
		row := b.DB.Pool.QueryRow(ctx, `select chat_id from issues where id=$1`, issueID)
		var userChat int64
		if err := row.Scan(&userChat); err == nil {
			b.reply(userChat, fmt.Sprintf("Статус вашей заявки #%d изменён на: %s", issueID, newStatus))
		}
		return
	}

	if strings.HasPrefix(data, "comment:") {
		parts := strings.Split(data, ":")
		if len(parts) != 2 {
			return
		}
		issueID, _ := strconv.ParseInt(parts[1], 10, 64)
		if ok, _ := b.DB.IsAdmin(ctx, cq.From.ID); !ok {
			b.answerCallback(cq, "Нет прав")
			return
		}
		b.pendingComments[cq.From.ID] = issueID
		b.answerCallback(cq, fmt.Sprintf("Напишите комментарий к заявке #%d", issueID))
		return
	}

	if strings.HasPrefix(data, "broadcast:") {
		if strings.HasSuffix(data, "confirm") {
			text, ok := b.pendingBroadcast[cq.From.ID]
			if !ok {
				b.answerCallback(cq, "Нет черновика")
				return
			}
			go b.sendBroadcast(ctx, cq.From.ID, text)
			delete(b.pendingBroadcast, cq.From.ID)
			b.answerCallback(cq, "Рассылка запущена")
			return
		}
		if strings.HasSuffix(data, "cancel") {
			delete(b.pendingBroadcast, cq.From.ID)
			b.answerCallback(cq, "Отменено")
			return
		}
	}
}

func (b *Bot) sendBroadcast(ctx context.Context, adminTG int64, text string) {
	ids, err := b.DB.ListAllChatIDs(ctx)
	if err != nil {
		return
	}
	sent := 0
	for _, id := range ids {
		_, err := b.API.Send(tgbotapi.NewMessage(id, text))
		if err == nil {
			sent++
		}
		time.Sleep(40 * time.Millisecond)
	}
	b.reply(adminTG, fmt.Sprintf("Рассылка доставлена: %d чатов", sent))
}

func (b *Bot) reply(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	b.API.Send(msg)
}

func (b *Bot) answerCallback(cq *tgbotapi.CallbackQuery, text string) {
	b.API.Request(tgbotapi.NewCallback(cq.ID, text))
}

func strPtrEmptyToNil(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// saveTelegramFile скачивает файл по file_id и сохраняет его в папку uploads
// Возвращает относительный путь
func (b *Bot) saveTelegramFile(fileID string, suggestedName string) (string, error) {
	cfg := tgbotapi.FileConfig{FileID: fileID}
	tgFile, err := b.API.GetFile(cfg)
	if err != nil {
		return "", err
	}

	url := tgFile.Link(b.API.Token)

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if err := os.MkdirAll("uploads", 0755); err != nil {
		return "", err
	}

	if suggestedName == "" {
		suggestedName = filepath.Base(tgFile.FilePath)
		if suggestedName == "" {
			suggestedName = fmt.Sprintf("%d.bin", time.Now().UnixNano())
		}
	}

	path := filepath.Join("uploads", suggestedName)

	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", err
	}

	return path, nil
}

func makeDistrictKeyboard() tgbotapi.ReplyKeyboardMarkup {
	rows := [][]tgbotapi.KeyboardButton{
		{tgbotapi.NewKeyboardButton(districts[0])},
		{tgbotapi.NewKeyboardButton(districts[1])},
		{tgbotapi.NewKeyboardButton(districts[2])},
		{tgbotapi.NewKeyboardButton(districts[3])},
	}
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	return kb
}

func makeCategoryKeyboard() tgbotapi.ReplyKeyboardMarkup {
	rows := [][]tgbotapi.KeyboardButton{
		{tgbotapi.NewKeyboardButton(categories[0])},
		{tgbotapi.NewKeyboardButton(categories[1])},
		{tgbotapi.NewKeyboardButton(categories[2])},
		{tgbotapi.NewKeyboardButton(categories[3])},
		{tgbotapi.NewKeyboardButton(categories[4])},
		{tgbotapi.NewKeyboardButton(categories[5])},
	}
	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	return kb
}

// меню выбора района для фильтра /issues_filter
func (b *Bot) sendIssuesFilterDistrictMenu(chatID int64) {
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, d := range districts {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(d, "if:d:"+d),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Все районы", "if:d:ALL"),
	))

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msg := tgbotapi.NewMessage(chatID, "Выберите район для фильтрации заявок:")
	msg.ReplyMarkup = kb
	b.API.Send(msg)
}

// makeAdminPagingKeyboard создает клавиатуру для пагинации в /issues
func makeAdminPagingKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅ Предыдущая"),
			tgbotapi.NewKeyboardButton("Следующая ➡"),
		),
	)
}

// hasIssueContent проверяет есть ли в сообщении содержимое для заявки
// текст, подпись к медиа или сами медиа. Геопозиция сюда НЕ входит.
func hasIssueContent(m *tgbotapi.Message) bool {
	if strings.TrimSpace(m.Text) != "" {
		return true
	}
	if strings.TrimSpace(m.Caption) != "" {
		return true
	}
	if len(m.Photo) > 0 {
		return true
	}
	if m.Document != nil || m.Video != nil || m.Audio != nil || m.Voice != nil || m.Animation != nil {
		return true
	}
	return false
}
