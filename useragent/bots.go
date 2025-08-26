// bots.go генерирует заголовки для поисковых ботов

package useragent

import (
	"fmt"
	"time"
)

// CrawlerType определяет тип поискового бота
type CrawlerType int

const (
	// GoogleBot имитирует десктопный Googlebot
	GoogleBot CrawlerType = iota
	// BingBot имитирует BingBot
	BingBot
	// YandexBot имитирует YandexBot
	YandexBot
)

// getCrawlerHeadersWithVersion создает заголовки для указанного типа краулера
// noinspection HttpUrlsUsage
func (g *Generator) getCrawlerHeadersWithVersion(crawlerType CrawlerType, chromeVersion string) map[string]string {
	var userAgent string
	headers := map[string]string{
		"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"accept-encoding": "gzip, deflate, br",
		"accept-language": "en-US,en;q=0.9", // в основном боты используют американский английский
	}
	defaultBotUserAgent := fmt.Sprintf(
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/%s Safari/537.36", // #nosec G107
		chromeVersion,
	)
	switch crawlerType {
	case GoogleBot:
		userAgent = defaultBotUserAgent
		headers["from"] = "googlebot(at)google.com"
	case BingBot:
		userAgent = fmt.Sprintf(
			"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm) Chrome/%s Safari/537.36", // #nosec G107
			chromeVersion,
		)
	case YandexBot:
		userAgent = "Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)" // #nosec G107
	default:
		userAgent = defaultBotUserAgent
		headers["from"] = "googlebot(at)google.com"
	}

	headers["user-agent"] = userAgent
	return headers
}

// GetCrawlerHeaders генерирует минимальный набор HTTP-заголовков для указанного поискового бота
//
// ВАЖНО: продвинутые системы защиты проверяют не только User-Agent, но и IP-адрес
// запроса с помощью обратного DNS-запроса (rDNS)!
//
// для успешной имитации бота на таких сайтах, запрос должен исходить из подсети,
// принадлежащей поисковой системе (например, в Google Cloud или Google Colab для имитации Googlebot)
func (g *Generator) GetCrawlerHeaders(crawlerType CrawlerType) map[string]string {
	g.mu.RLock()
	// проверка версий в кэше
	if len(g.versions) == 0 {
		g.mu.RUnlock()
		// маловероятная ситуация: Generator всегда возвращает актуальные версии
		latestVersion := approximateVersionForDate(time.Now()) // фоллбэк на аппроксимацию на основе даты
		return g.getCrawlerHeadersWithVersion(crawlerType, latestVersion)
	}

	latestVersion := g.versions[0] // боты Google и Bing стремятся использовать последние версии Chromium
	g.mu.RUnlock()

	return g.getCrawlerHeadersWithVersion(crawlerType, latestVersion)
}
