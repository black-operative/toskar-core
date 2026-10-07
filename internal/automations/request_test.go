package automations

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/locale"
)

// The same cases as web/src/features/automations/parseRequest.test.ts, so
// the daemon reads requests as the form did (#204).

const requestZone = "America/Los_Angeles"

var (
	// Monday, September 28, 2026, 8:00 AM in Los Angeles.
	requestNow   = time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	tomorrowAt9  = time.Date(2026, 9, 29, 16, 0, 0, 0, time.UTC)
	requestAlert = Notification{Mode: NotifyAlways}
)

func mustParse(t *testing.T, text, lang string) ParsedRequest {
	t.Helper()
	parsed, err := ParseRequest(text, requestNow, requestZone, lang)
	if err != nil {
		t.Fatalf("ParseRequest(%q, %s): %v", text, lang, err)
	}
	return parsed
}

func threshold(op string, value float64, currency string) Notification {
	return Notification{Mode: NotifyOnCondition, Condition: &Condition{Kind: ConditionThreshold, Op: op, Value: value, Currency: currency}}
}

var (
	availableNote = Notification{Mode: NotifyOnCondition, Condition: &Condition{Kind: ConditionAvailable}}
	changeNote    = Notification{Mode: NotifyOnChange}
	noneNote      = Notification{Mode: NotifyNone}
)

func sameNotification(a, b Notification) bool {
	if a.Mode != b.Mode || (a.Condition == nil) != (b.Condition == nil) {
		return false
	}
	return a.Condition == nil || *a.Condition == *b.Condition
}

// want checks the schedule fields a case sets.
type want struct {
	kind    Kind
	hour    *int
	minute  *int
	weekday *int
	every   int
	at      *time.Time
}

func intp(v int) *int { return &v }

func (w want) check(t *testing.T, label string, s Schedule) {
	t.Helper()
	if w.kind != "" && s.Kind != w.kind {
		t.Errorf("%s: kind = %s, want %s", label, s.Kind, w.kind)
	}
	if w.hour != nil && s.Hour != *w.hour {
		t.Errorf("%s: hour = %d, want %d", label, s.Hour, *w.hour)
	}
	if w.minute != nil && s.Minute != *w.minute {
		t.Errorf("%s: minute = %d, want %d", label, s.Minute, *w.minute)
	}
	if w.weekday != nil && (s.Weekday == nil || *s.Weekday != *w.weekday) {
		t.Errorf("%s: weekday = %v, want %d", label, s.Weekday, *w.weekday)
	}
	if w.every != 0 && s.EverySeconds != w.every {
		t.Errorf("%s: every = %d, want %d", label, s.EverySeconds, w.every)
	}
	if w.at != nil && (s.At == nil || !s.At.Equal(*w.at)) {
		t.Errorf("%s: at = %v, want %s", label, s.At, w.at)
	}
}

func daily(h, m int) want { return want{kind: KindDaily, hour: intp(h), minute: intp(m)} }
func weekly(d, h, m int) want {
	return want{kind: KindWeekly, weekday: intp(d), hour: intp(h), minute: intp(m)}
}
func every(s int) want       { return want{kind: KindInterval, every: s} }
func once(at time.Time) want { return want{kind: KindOnce, at: &at} }

func TestParseRequestEnglish(t *testing.T) {
	parsed := mustParse(t, "Every morning at 8:00 AM, check this product and tell me if the price is below $500.", "en")
	if parsed.Name != "Price below $500" {
		t.Errorf("name = %q", parsed.Name)
	}
	daily(8, 0).check(t, "price check", parsed.Schedule)
	if parsed.Schedule.TimeZone != requestZone {
		t.Errorf("zone = %q", parsed.Schedule.TimeZone)
	}
	if !sameNotification(parsed.Notification, threshold(OpBelow, 500, "USD")) {
		t.Errorf("notification = %+v", parsed.Notification)
	}
	if parsed.Prompt != "Check this product. Report the current price." {
		t.Errorf("prompt = %q", parsed.Prompt)
	}
	if len(parsed.Notes) != 0 {
		t.Errorf("notes = %v", parsed.Notes)
	}

	stock := mustParse(t, "Every six hours, check whether this item is back in stock. Notify me only when it becomes available.", "en")
	if stock.Name != "Stock check" || !sameNotification(stock.Notification, availableNote) {
		t.Errorf("stock = %+v", stock)
	}
	every(6*3600).check(t, "stock", stock.Schedule)

	release := mustParse(t, "Every Friday, check for new releases of this software and summarize what changed.", "en")
	if release.Name != "Release check" || release.Notification.Mode != NotifyAlways {
		t.Errorf("release = %+v", release)
	}
	weekly(5, 8, 0).check(t, "release", release.Schedule)
	if len(release.Notes) != 1 || release.Notes[0] != "No time was given, so this runs at 8:00 AM." {
		t.Errorf("notes = %v", release.Notes)
	}

	research := mustParse(t, "Run this research prompt once tomorrow at 9:00 AM.", "en")
	if research.Name != "Research" {
		t.Errorf("name = %q", research.Name)
	}
	once(tomorrowAt9).check(t, "research", research.Schedule)

	quiet := mustParse(t, "Don't notify me. Every day at 7:00 AM, check the news.", "en")
	if quiet.Notification.Mode != NotifyNone {
		t.Errorf("quiet = %+v", quiet.Notification)
	}
	daily(7, 0).check(t, "quiet", quiet.Schedule)
	if change := mustParse(t, "Every day at 7:00 AM, notify me only when the page changes.", "en"); change.Notification.Mode != NotifyOnChange {
		t.Errorf("change = %+v", change.Notification)
	}

	daily(18, 30).check(t, "24-hour", mustParse(t, "Every day at 18:30, check the news.", "en").Schedule)
	daily(18, 30).check(t, "evening", mustParse(t, "Every evening at 6:30, summarize the news.", "en").Schedule)
	every(3600).check(t, "hourly", mustParse(t, "Every hour, check the queue.", "en").Schedule)
	if n := mustParse(t, "Every day, tell me if the price is above 1,299.99 EUR.", "en").Notification; !sameNotification(n, threshold(OpAbove, 1299.99, "EUR")) {
		t.Errorf("euro = %+v", n.Condition)
	}
}

func TestParseRequestEnglishInAnotherLanguage(t *testing.T) {
	parsed := mustParse(t, "Every morning at 8:00 AM, tell me if the price is below 500.", "de")
	daily(8, 0).check(t, "english in de", parsed.Schedule)
	if !sameNotification(parsed.Notification, threshold(OpBelow, 500, "EUR")) {
		t.Errorf("notification = %+v", parsed.Notification.Condition)
	}
	if parsed.Prompt != "Tell me if the price is below 500." {
		t.Errorf("prompt = %q", parsed.Prompt)
	}

	kept := mustParse(t, "Täglich um 7:30 Uhr prüfen, ob der Preis unter 19,99 € fällt.", "de")
	daily(7, 30).check(t, "kept clause", kept.Schedule)
	if !sameNotification(kept.Notification, threshold(OpBelow, 19.99, "EUR")) {
		t.Errorf("notification = %+v", kept.Notification.Condition)
	}
	if kept.Prompt != "Täglich um 7:30 Uhr prüfen, ob der Preis unter 19,99 € fällt." {
		t.Errorf("prompt = %q", kept.Prompt)
	}
}

func TestParseRequestErrors(t *testing.T) {
	if _, err := ParseRequest("Check the news.", requestNow, requestZone, "en"); !errors.Is(err, ErrRequestWhen) {
		t.Errorf("no schedule: %v", err)
	}
	if _, err := ParseRequest("Jeden Morgen um 8 Uhr die Nachrichten prüfen.", requestNow, requestZone, "en"); err == nil {
		t.Error("German read without the German words")
	}
	if _, err := ParseRequest("  ", requestNow, requestZone, "en"); !errors.Is(err, ErrRequestEmpty) {
		t.Errorf("empty: %v", err)
	}
	if _, err := ParseRequest("Every day at 8", requestNow, "", "en"); !errors.Is(err, ErrRequestTimeZone) {
		t.Errorf("no zone: %v", err)
	}
}

func TestReadNumber(t *testing.T) {
	for text, want := range map[string]float64{"1,299.99": 1299.99, "1.299,99": 1299.99, "1 299,99": 1299.99, "2.500": 2500, "4.99": 4.99, "19,5": 19.5} {
		if got, ok := readNumber(text); !ok || got != want {
			t.Errorf("readNumber(%q) = %v %v, want %v", text, got, ok, want)
		}
	}
	if _, ok := readNumber("about 5"); ok {
		t.Error("read 'about 5' as a number")
	}
}

type requestCase struct {
	text         string
	schedule     want
	notification *Notification
}

func notep(n Notification) *Notification { return &n }

var requestLanguageCases = map[string]struct {
	exampleValue    float64
	exampleCurrency string
	task            string
	cases           []requestCase
}{
	"de": {500, "EUR", "Prüfe dieses Produkt. Nenne den aktuellen Preis.", []requestCase{
		{"Jeden Freitag um 18:30 die Release-Notes zusammenfassen.", weekly(5, 18, 30), nil},
		{"Freitags um 7 Uhr den Wetterbericht holen.", weekly(5, 7, 0), nil},
		{"Alle zwei Stunden prüfen, ob der Artikel wieder auf Lager ist.", every(7200), notep(availableNote)},
		{"Stündlich die Warteschlange prüfen.", every(3600), nil},
		{"Morgen um 9 Uhr einmal diese Recherche ausführen.", once(tomorrowAt9), nil},
		{"Heute Morgen um 11 Uhr die Bestellung prüfen.", once(time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)), nil},
		{"Jeden Abend um 8 die Nachrichten zusammenfassen, nur bei Änderungen.", daily(20, 0), notep(changeNote)},
		{"Täglich um 18 Uhr den Bericht speichern, nicht benachrichtigen.", daily(18, 0), notep(noneNote)},
		{"Jeden Tag Bescheid geben, wenn der Preis über 1.299,99 € steigt.", daily(8, 0), notep(threshold(OpAbove, 1299.99, "EUR"))},
	}},
	"es": {500, "EUR", "Revisa este producto. Indica el precio actual.", []requestCase{
		{"Todos los lunes a las 18:30, revisa las novedades.", weekly(1, 18, 30), nil},
		{"Cada 30 minutos, comprueba si vuelve a estar disponible.", every(1800), notep(availableNote)},
		{"Mañana a las 9, ejecuta esta investigación una vez.", once(tomorrowAt9), nil},
		{"Hoy por la mañana a las 11, revisa el pedido.", once(time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)), nil},
		{"Todas las noches a las 10, resume las noticias y avísame solo si hay cambios.", daily(22, 0), notep(changeNote)},
		{"Cada día a las 8 y media, avísame si el precio supera los 80 dólares.", daily(8, 30), notep(threshold(OpAbove, 80, "USD"))},
	}},
	"fr": {500, "EUR", "Vérifie ce produit. Indique le prix actuel.", []requestCase{
		{"Tous les vendredis à 18 h 30, résume les nouveautés.", weekly(5, 18, 30), nil},
		{"Toutes les 2 heures, vérifie si l’article est de nouveau en stock.", every(7200), notep(availableNote)},
		{"Demain à 9 h, lance cette recherche une fois.", once(tomorrowAt9), nil},
		{"Chaque soir à 8 heures, préviens-moi si le prix dépasse 1 299,99 €.", daily(20, 0), notep(threshold(OpAbove, 1299.99, "EUR"))},
		{"Tous les jours à 18h, résume les actualités et préviens-moi quand ça change.", daily(18, 0), notep(changeNote)},
	}},
	"it": {500, "EUR", "Controlla questo prodotto. Indica il prezzo attuale.", []requestCase{
		{"Ogni venerdì alle 18:30, riassumi le novità.", weekly(5, 18, 30), nil},
		{"Ogni 15 minuti controlla se è di nuovo disponibile.", every(900), notep(availableNote)},
		{"Domani alle 9 esegui questa ricerca una volta.", once(tomorrowAt9), nil},
		{"Ogni sera alle 8 riassumi le notizie e avvisami solo quando cambia.", daily(20, 0), notep(changeNote)},
	}},
	"pt-BR": {2500, "BRL", "Verifique este produto. Informe o preço atual.", []requestCase{
		{"Toda sexta-feira às 18:30, resuma as novidades.", weekly(5, 18, 30), nil},
		{"A cada 6 horas, veja se o item está em estoque.", every(6 * 3600), notep(availableNote)},
		{"Amanhã às 9, execute esta pesquisa uma vez.", once(tomorrowAt9), nil},
		{"Todo dia às 8 da noite, me avise só quando mudar.", daily(20, 0), notep(changeNote)},
		{"Todos os dias às 7h, me avise se o preço passar de 2 mil reais.", daily(7, 0), notep(threshold(OpAbove, 2000, "BRL"))},
	}},
	"ja": {50000, "JPY", "この商品をチェックして、価格が5万円を下回ったら教えてください。", []requestCase{
		{"毎週金曜日の18時30分に、リリース情報をまとめて。", weekly(5, 18, 30), nil},
		{"2時間ごとに在庫を確認して、再入荷したら通知して。", every(7200), notep(availableNote)},
		{"明日の午後3時に一度だけ実行して。", once(time.Date(2026, 9, 29, 22, 0, 0, 0, time.UTC)), nil},
		{"毎日18時に確認して、変わったら通知して。", daily(18, 0), notep(changeNote)},
		{"毎晩8時半にニュースをまとめて。", daily(20, 30), nil},
		{"毎日、価格が1000ドル以上になったら教えて。", daily(8, 0), notep(threshold(OpAbove, 1000, "USD"))},
	}},
	"ko": {500000, "KRW", "이 상품을 확인하고 가격이 50만 원 아래로 내려가면 알려 주세요.", []requestCase{
		{"매주 금요일 오후 6시 30분에 새 버전을 확인해 줘.", weekly(5, 18, 30), nil},
		{"3시간마다 재고를 확인하고 재입고되면 알려 줘.", every(3 * 3600), notep(availableNote)},
		{"내일 오전 9시에 한 번 실행해 줘.", once(tomorrowAt9), nil},
		{"매일 저녁 7시에 뉴스를 요약하고 바뀌면 알려 줘.", daily(19, 0), notep(changeNote)},
		{"매일 18시에 확인하고 가격이 10만 원 이상이면 알려 줘.", daily(18, 0), notep(threshold(OpAbove, 100000, "KRW"))},
	}},
	"zh-Hans": {3000, "CNY", "检查这个商品，如果价格低于3000元就告诉我。", []requestCase{
		{"每周五晚上6点半，总结本周的新版本。", weekly(5, 18, 30), nil},
		{"每两小时检查一次库存，有货时通知我。", every(7200), notep(availableNote)},
		{"明天下午3点运行一次。", once(time.Date(2026, 9, 29, 22, 0, 0, 0, time.UTC)), nil},
		{"每天晚上八点，有变化时通知我。", daily(20, 0), notep(changeNote)},
		{"每天18:30检查，价格高于¥1万时告诉我。", daily(18, 30), notep(threshold(OpAbove, 10000, "CNY"))},
	}},
	"zh-Hant": {3000, "TWD", "檢查這個商品，如果價格低於3000元就告訴我。", []requestCase{
		{"每週五晚上6點半，總結本週的新版本。", weekly(5, 18, 30), nil},
		{"每三個小時檢查庫存，補貨時通知我。", every(3 * 3600), notep(availableNote)},
		{"每天晚上9點，價格高於1萬元時通知我。", daily(21, 0), notep(threshold(OpAbove, 10000, "TWD"))},
	}},
}

func TestParseRequestInEveryLanguage(t *testing.T) {
	for lang, l := range requestLanguageCases {
		t.Run(lang, func(t *testing.T) {
			// The example request in the box, in that language.
			placeholder := locale.T(lang, "automations:form.describePlaceholder", nil)
			if strings.Contains(strings.ToLower(placeholder), "every morning") {
				t.Fatalf("no %s example: %q", lang, placeholder)
			}
			example := mustParse(t, placeholder, lang)
			daily(8, 0).check(t, "example", example.Schedule)
			if !sameNotification(example.Notification, threshold(OpBelow, l.exampleValue, l.exampleCurrency)) {
				t.Errorf("example notification = %+v", example.Notification.Condition)
			}
			if len(example.Notes) != 0 {
				t.Errorf("example notes = %v", example.Notes)
			}
			if l.task != "" && example.Prompt != l.task {
				t.Errorf("example task = %q, want %q", example.Prompt, l.task)
			}
			for _, c := range l.cases {
				parsed := mustParse(t, c.text, lang)
				c.schedule.check(t, c.text, parsed.Schedule)
				wantNote := requestAlert
				if c.notification != nil {
					wantNote = *c.notification
				}
				if !sameNotification(parsed.Notification, wantNote) {
					t.Errorf("%s: notification = %+v %+v, want %+v %+v", c.text, parsed.Notification.Mode, parsed.Notification.Condition, wantNote.Mode, wantNote.Condition)
				}
			}
			// English still reads.
			weekly(5, 18, 30).check(t, "english", mustParse(t, "Every Friday at 6:30 PM, check for new releases.", lang).Schedule)
		})
	}
}

func TestEveryLanguageHasRequestWords(t *testing.T) {
	for _, lang := range locale.Languages() {
		if strings.HasPrefix(lang, "en-X") {
			continue
		}
		if requestWordsFor(lang) == nil {
			t.Errorf("no request words for %s", lang)
		}
	}
}

// The page's ideas (web/src/features/automations/ideas.ts, with the same
// expectations) must fill in the form as intended in every language.
func TestIdeaRequestsInEveryLanguage(t *testing.T) {
	ideas := map[string]struct {
		kind      Kind
		mode      NotifyMode
		condition string
	}{
		"news":     {KindDaily, NotifyAlways, ""},
		"price":    {KindDaily, NotifyOnCondition, ConditionThreshold},
		"stock":    {KindInterval, NotifyOnCondition, ConditionAvailable},
		"releases": {KindWeekly, NotifyAlways, ""},
		"page":     {KindDaily, NotifyOnChange, ""},
	}
	for _, lang := range locale.Languages() {
		if strings.HasPrefix(lang, "en-X") {
			continue
		}
		for id, want := range ideas {
			request := locale.T(lang, "automations:ideas."+id+".request", nil)
			parsed, err := ParseRequest(request, requestNow, requestZone, lang)
			if err != nil {
				t.Errorf("%s %s %q: %v", lang, id, request, err)
				continue
			}
			if parsed.Schedule.Kind != want.kind || parsed.Notification.Mode != want.mode {
				t.Errorf("%s %s: %s/%s, want %s/%s", lang, id, parsed.Schedule.Kind, parsed.Notification.Mode, want.kind, want.mode)
			}
			if want.condition != "" && (parsed.Notification.Condition == nil || parsed.Notification.Condition.Kind != want.condition) {
				t.Errorf("%s %s: condition %+v, want %s", lang, id, parsed.Notification.Condition, want.condition)
			}
			if len(parsed.Notes) != 0 {
				t.Errorf("%s %s: notes %v", lang, id, parsed.Notes)
			}
		}
	}
}
