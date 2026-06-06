// headers.go генерирует набор правдоподобных HTTP-заголовков, имитирующих запрос браузера

package useragent

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	uaMajorVersionRegex = regexp.MustCompile(`Chrome/(\d+)`)
	uaFullVersionRegex  = regexp.MustCompile(`Chrome/(\d+\.\d+\.\d+\.\d+)`)
	uaPlatformRegex     = regexp.MustCompile(`\(([^;]+)`)
	deviceMemories      = []string{"4", "8", "16", "32"}
	dprs                = []string{"1.0", "1.25", "1.5", "2.0"}
	rtts                = []string{"50", "100", "150", "200"}
	downlinks           = []string{"1.5", "2.0", "5.8", "8.0", "9.9", "10.0"}
)

// greaseChars содержит разрешенные символы в GREASE-бренде.
const greaseChars = ` ;;:/??==()__-,."` // повторы для повышения вероятности выбора

// generateGreaseBrand создает случайный "GREASE" бренд для заголовка sec-ch-ua,
// что делает отпечаток менее статичным и более похожим на реальный браузер.
// подробнее: https://wicg.github.io/ua-client-hints/#grease
// https://chromium-review.googlesource.com/c/chromium/src/+/2181733
func generateGreaseBrand() (brand string, version string) {
	// 99 встречается чаще, повторы для повышения вероятности выбора
	greaseVersions := []string{"8", "24", "99", "99", "99", "99"}
	version = greaseVersions[rand.IntN(len(greaseVersions))]
	// случайное имя бренда, заменяя пробелы спецсимволами
	baseBrand := "Not A Brand"
	var sb strings.Builder
	sb.Grow(len(baseBrand))

	for _, char := range baseBrand {
		if char == ' ' {
			sb.WriteByte(greaseChars[rand.IntN(len(greaseChars))])
		} else {
			sb.WriteRune(char)
		}
	}
	brand = sb.String()
	return
}

// screenResolution описывает разрешение экрана
type screenResolution struct {
	Width  int
	Height int
}

// commonResolutions содержит список популярных разрешений для десктопов.
// https://gs.statcounter.com/screen-resolution-stats/desktop/worldwide
var commonResolutions = []screenResolution{
	{1920, 1080}, // ~24%
	{1366, 768},  // ~11%
	{1536, 864},  // ~11%
	{1280, 720},  // ~6%
	{1440, 900},  // ~4%
	{2560, 1440}, // ~3%
}

// вьюпорт (viewport, с англ. — «окно просмотра») никогда не может быть равен размерам экрана, он всегда меньше, и нужно учесть:
// типичный заголовок окна с панелью поиска/инструментов: 90px в Edge или 128 в Chrome +
// 60px высота панели задач Windows 11
// ширина окна браузера уменьшается за счет боковых панелей в Edge на 64px или 128px в если включены боковые вкладки
var (
	viewportHeightSubtractions = []int{90, 128, 150, 188} // панели инструментов/поиска, заголовки, панель задач ОС
	viewportWidthSubtractions  = []int{2, 4, 64, 128}     // cкроллбар, боковые панели, рамки окна
)

// browserInfo хранит разобранные данные из строки User-Agent
type browserInfo struct {
	UserAgent    string
	MajorVersion string
	FullVersion  string
	Platform     string // "Windows" || "Linux"
	BrandName    string // "Google Chrome" || "Microsoft Edge"
	SecBrandName string // "Google Chrome" || "Microsoft Edge"
}

// parseUserAgent извлекает структурированную информацию из строки User-Agent
func parseUserAgent(ua string) browserInfo {
	info := browserInfo{UserAgent: ua}

	// 1. извлечение версий
	if match := uaMajorVersionRegex.FindStringSubmatch(ua); len(match) > 1 {
		info.MajorVersion = match[1]
	}
	if match := uaFullVersionRegex.FindStringSubmatch(ua); len(match) > 1 {
		info.FullVersion = match[1]
	} else {
		info.FullVersion = info.MajorVersion // фоллбэк на мажорную версию
	}

	// 2. извлечение платформы
	if match := uaPlatformRegex.FindStringSubmatch(ua); len(match) > 1 {
		platformStr := strings.Fields(match[1])[0]
		if strings.EqualFold(platformStr, "windows") {
			info.Platform = "Windows"
		} else {
			info.Platform = "Linux"
		}
	} else {
		info.Platform = "Windows"
	}

	// 3. определение бренда
	if strings.Contains(ua, "Edg/") {
		info.BrandName = "Microsoft Edge"
		info.SecBrandName = "Microsoft Edge"
	} else {
		info.BrandName = "Google Chrome"
		info.SecBrandName = "Google Chrome"
	}

	return info
}

// GetHeaders генерирует набор правдоподобных HTTP-заголовков, имитирующих запрос браузера.
// Он принимает необязательный URL, который используется для формирования заголовков
// 'Referer' и 'Origin'. Если URL не указан, используется 'https://google.com'
// в качестве запасного варианта для 'Referer', а 'Origin' опускается.
// Возвращаемая карта может быть безопасно изменена вызывающей стороной.
func (g *Generator) GetHeaders(targetURL ...string) map[string]string {
	ua := g.Get()
	info := parseUserAgent(ua)

	var referer, origin string
	var hasURL bool
	var secFetchSite = "none"
	var u *url.URL
	var err error
	if len(targetURL) > 0 && targetURL[0] != "" {
		u, err = url.Parse(targetURL[0])
		hasURL = err == nil
	}

	if !hasURL {
		u, _ = url.Parse("https://www.google.com/search?q=")
		secFetchSite = "cross-site"
	}

	referer = fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	if hasURL {
		origin = referer
		secFetchSite = "same-origin"
	}

	// динамическая генерация sec-ch-ua
	greaseBrand, greaseVersion := generateGreaseBrand()

	// генерация полных версий для sec-ch-ua-full-version-list
	greaseFullVersion := fmt.Sprintf("%s.0.0.0", greaseVersion)

	secChUaFullList := fmt.Sprintf(
		`"%s";v="%s", "%s";v="%s", "%s";v="%s"`,
		info.SecBrandName, info.FullVersion,
		greaseBrand, greaseFullVersion,
		"Chromium", info.FullVersion,
	)

	secChUa := fmt.Sprintf(
		`"%s";v="%s", "%s";v="%s", "%s";v="%s"`,
		info.SecBrandName, info.MajorVersion,
		greaseBrand, greaseVersion,
		"Chromium", info.MajorVersion,
	)

	// рандомизация железа и сети
	deviceMemory := deviceMemories[rand.IntN(len(deviceMemories))]
	dpr := dprs[rand.IntN(len(dprs))]
	rtt := rtts[rand.IntN(len(rtts))]
	downlink := downlinks[rand.IntN(len(downlinks))]

	// случайное разрешение экрана
	resolution := commonResolutions[rand.IntN(len(commonResolutions))]

	// случайное значение для панелей инструментов и т.д.
	heightSubtraction := viewportHeightSubtractions[rand.IntN(len(viewportHeightSubtractions))]
	widthSubtraction := viewportWidthSubtractions[rand.IntN(len(viewportWidthSubtractions))]

	// вычисление размеров вьюпорта
	viewportHeight := strconv.Itoa(resolution.Height - heightSubtraction)
	viewportWidth := strconv.Itoa(resolution.Width - widthSubtraction)

	headers := map[string]string{
		"user-agent":                  ua,
		"accept":                      "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"accept-language":             "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7",
		"device-memory":               deviceMemory,
		"downlink":                    downlink,
		"dpr":                         dpr,
		"ect":                         "4g",
		"rtt":                         rtt,
		"referer":                     referer,
		"cache-control":               "no-cache",
		"pragma":                      "no-cache",
		"sec-ch-ua":                   secChUa,
		"sec-ch-ua-arch":              `"x86"`,
		"sec-ch-ua-bitness":           `"64"`,
		"sec-ch-ua-full-version":      fmt.Sprintf(`"%s"`, info.FullVersion),
		"sec-ch-ua-full-version-list": secChUaFullList,
		"sec-ch-ua-mobile":            "?0",
		"sec-ch-ua-model":             `""`,
		"sec-ch-ua-platform":          fmt.Sprintf(`"%s"`, info.Platform),
		"sec-ch-ua-platform-version":  `"19.0.0"`,
		"sec-ch-ua-wow64":             "?0",
		"sec-ch-viewport-height":      viewportHeight,
		"sec-ch-viewport-width":       viewportWidth,
		"viewport-width":              viewportWidth,
		"sec-fetch-dest":              "document",
		"sec-fetch-mode":              "navigate",
		"sec-fetch-site":              secFetchSite, // если нет реферера - "none", если есть - "cross-site" или "same-origin"
		"sec-fetch-user":              "?1",
		"upgrade-insecure-requests":   "1",
		"priority":                    "u=0, i",
	}

	if origin != "" {
		headers["origin"] = origin
	}

	return headers
}
