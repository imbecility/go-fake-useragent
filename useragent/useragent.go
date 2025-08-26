// useragent.go генерация случайных и актуальных User-Agent строк

// Package useragent генерирует случайные и актуальные User-Agent строки:
// использует сетевые запросы для обновления списка версий из Google и Microsoft,
// дисковое кэширование и математическую аппроксимацию -
// чтобы при любых обстоятельствах возвращать строку User-Agent.
package useragent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// источники данных
	googleAPIURL  = "https://versionhistory.googleapis.com/v1/chrome/platforms/win64/channels/stable/versions/all/releases"
	msEdgeRepoURL = "https://packages.microsoft.com/repos/edge/pool/main/m/microsoft-edge-stable"

	// количество версий для каждого источника
	versionsToKeepFromGoogle = 45
	versionsToKeepFromMS     = 20

	// шаблоны User-Agent
	chromeUATemplate = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36"
	edgeUATemplate   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 Edg/%s"

	// имя файла для дискового кэша по умолчанию
	defaultCacheFileName = "go_ua_versions.json"
)

// регулярное выражение для парсинга версий MS Edge со страницы
var msEdgeVersionRegex = regexp.MustCompile(
	`<a href="([^"]+\.deb)">[^<]+</a>\s+(\d{1,2}-[A-Za-z]{3}-\d{4})\s+(\d{1,2}:\d{2})`,
)

// googleAPIResponse структура для парсинга ответа Google Versions API
type googleAPIResponse struct {
	Releases []struct {
		Version string `json:"version"`
	} `json:"releases"`
}

// cacheFile структура для сохранения версий в дисковом кэше
type cacheFile struct {
	Timestamp time.Time `json:"timestamp"`
	Versions  []string  `json:"versions"`
}

// msEdgeRelease содержит информацию, извлеченную из репозитория Microsoft Edge
type msEdgeRelease struct {
	Version string
	Date    time.Time
}

// Option настраивает Generator
type Option func(*Generator)

// Generator - потокобезопасный генератор для случайных строк User-Agent
type Generator struct {
	versions []string
	mu       sync.RWMutex

	httpClient    *http.Client
	logger        *slog.Logger
	diskCachePath string
	diskCacheTTL  time.Duration
}

// WithHTTPClient устанавливает пользовательский клиент для генератора
func WithHTTPClient(client *http.Client) Option {
	return func(g *Generator) {
		if client != nil {
			g.httpClient = client
		}
	}
}

// WithLogger устанавливает пользовательский slog.Logger для генератора
func WithLogger(logger *slog.Logger) Option {
	return func(g *Generator) {
		if logger != nil {
			g.logger = logger
		}
	}
}

// loadFromDiskCache загружает версии из дискового кэша, если он актуален и содержит версии браузеров:
// возвращает true, если кэш был успешно загружен, иначе false
func (g *Generator) loadFromDiskCache() bool {
	data, err := os.ReadFile(g.diskCachePath)
	if err != nil {
		if !os.IsNotExist(err) {
			g.logger.Warn("не удалось прочитать кэш из файла", "path", g.diskCachePath, "error", err)
		}
		return false
	}

	var cache cacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		g.logger.Warn("не удалось распарсить кэш из файла", "path", g.diskCachePath, "error", err)
		return false
	}

	if time.Since(cache.Timestamp) > g.diskCacheTTL {
		g.logger.Debug("кэш на диске устарел и будет обновлен…", "path", g.diskCachePath)
		return false
	}

	if len(cache.Versions) == 0 {
		g.logger.Warn("кэш версий браузеров пуст", "path", g.diskCachePath)
		return false
	}

	g.mu.Lock()
	g.versions = cache.Versions
	g.mu.Unlock()
	return true
}

// saveToDiskCache сохраняет версии браузеров в дисковый кэш
func (g *Generator) saveToDiskCache() {
	g.mu.RLock()
	versionsToCache := g.versions
	g.mu.RUnlock()

	if len(versionsToCache) == 0 {
		g.logger.Warn("пропуск сохранения кеша диска, так как не было загружено ни одной версии")
		return
	}

	cache := cacheFile{
		Timestamp: time.Now(),
		Versions:  versionsToCache,
	}

	data, err := json.Marshal(cache)
	if err != nil {
		g.logger.Error("не удалось преобразовать версии из кеша на диске", "error", err)
		return
	}

	// атомарная запись через временный файл:
	// предотвращает повреждение кэш-файла, если программа завершится во время записи
	dir := filepath.Dir(g.diskCachePath)
	tempFile, err := os.CreateTemp(dir, "useragent-cache-*.tmp")
	if err != nil {
		g.logger.Error("не удалось создать временный файл для кэша", "error", err)
		return
	}

	// временный файл будет удален в конце, даже если запись не будет завершена
	defer func() {
		_ = os.Remove(tempFile.Name()) // ожидаемо это удаление завершится ошибкой, если переименование пройдет успешно
	}()

	if _, err := tempFile.Write(data); err != nil {
		g.logger.Error("не удалось записать версии браузеров во временный файл", "error", err)
		_ = tempFile.Close()
		return
	}

	if err := tempFile.Close(); err != nil {
		g.logger.Error("не удалось закрыть временный файл", "error", err)
		return
	}

	if err := os.Rename(tempFile.Name(), g.diskCachePath); err != nil {
		g.logger.Error(
			fmt.Sprintf("не удалось переименовать временный файл %s в %s", tempFile.Name(), g.diskCachePath), "error", err)
		return
	}

	g.logger.Debug("версии браузера сохранены в дисковый кэш", "path", g.diskCachePath)
}

// NewGenerator создаёт генератор User-Agent:
// загружает актуальные версии браузеров - сначала с диска (при наличии кэша),
// затем параллельно запрашивает данные у Google и Microsoft, а
// при ошибках сети формирует примерные значения на основе текущей даты
func NewGenerator(opts ...Option) (*Generator, error) {
	g := &Generator{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)), // по умолчанию используется тихий логгер
	}

	for _, opt := range opts {
		opt(g)
	}

	// 1. попытка загрузить из дискового кэша
	if g.diskCachePath != "" {
		if loaded := g.loadFromDiskCache(); loaded {
			g.logger.Debug("успешно загружены версии User-Agent из кэша на диске")
			return g, nil
		}
	}

	// 2. если кэш невалиден или отключен, используются данные из сетевых источников
	if err := g.updateVersions(); err != nil {
		// теоретически, этого никогда не произойдёт, из-за резервного варианта с аппроксимацией.
		return nil, fmt.Errorf("не удалось получить версии после всех резервных вариантов: %w", err)
	}

	// 3. если кэш включен, версии сохраняются на диск
	if g.diskCachePath != "" {
		g.saveToDiskCache()
	}

	return g, nil
}

// Get конкурентнобезопасно возвращает случайную, актуальную строку User-Agent для браузера Chrome или Edge
func (g *Generator) Get() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.versions) == 0 {
		// резервный вариант на случай маловероятной ситуации, когда инициализация частично завершилась неудачей, но не вернула ошибку.
		return fmt.Sprintf(chromeUATemplate, g.approximateVersions())
	}

	// выбор случайной версии из кэша
	randomVersion := g.versions[rand.IntN(len(g.versions))]

	// вероятность выбора Chrome - 50%, Edge - 50%
	if rand.IntN(2) == 0 {
		return fmt.Sprintf(chromeUATemplate, randomVersion)
	}
	return fmt.Sprintf(edgeUATemplate, randomVersion, randomVersion)
}

// WithDiskCache включает кеширование на диске для сохранения версий браузера между запусками приложения.
// path определяет, куда сохранять кэш (по умолчанию во временной директории системы).
// ttl определяет, как долго кеш считается действительным.
func WithDiskCache(path string, ttl time.Duration) Option {
	return func(g *Generator) {
		if path == "" {
			path = filepath.Join(os.TempDir(), defaultCacheFileName)
		}
		g.diskCachePath = path
		g.diskCacheTTL = ttl
	}
}

// fetchGoogleVersions получает последние версии Chrome через официальный API Google.
func (g *Generator) fetchGoogleVersions(ctx context.Context) (_ []string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleAPIURL, nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать запрос: %w", err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP запрос не удался: %w", err)
	}
	defer func() {
		// обработка закрытия тела HTTP-ответа, чтобы выявить проблемы ввода-вывода
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("неверный HTTP статус: %s", resp.Status)
	}

	var apiResponse googleAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("не удалось декодировать JSON-ответ: %w", err)
	}

	if len(apiResponse.Releases) == 0 {
		return nil, errors.New("API не вернул релизов")
	}

	limit := min(versionsToKeepFromGoogle, len(apiResponse.Releases))
	versions := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		versions = append(versions, apiResponse.Releases[i].Version)
	}

	return versions, nil
}

// fetchMicrosoftVersions парсит страницу репозитория Microsoft Edge, чтобы найти последние версии браузеров.
func (g *Generator) fetchMicrosoftVersions(ctx context.Context) (_ []string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, msEdgeRepoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать запрос: %w", err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP запрос не удался: %w", err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("неверный HTTP статус: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать тело ответа: %w", err)
	}

	matches := msEdgeVersionRegex.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		g.logger.Debug(string(body)) // логгирование всего тела страницы для отладки
		return nil, fmt.Errorf("не удалось найти версии браузеров на странице %s, возможно паттерн регулярного выражения устарел", msEdgeRepoURL)
	}

	releases := make([]msEdgeRelease, 0, len(matches))

	for _, match := range matches {
		// match[0] = вся строка
		// match[1] = filename (e.g., "microsoft-edge-stable_128.0.2739.25-1_amd64.deb")
		// match[2] = date_str (e.g., "20-Aug-2024")
		// match[3] = time_str (e.g., "20:31")
		if len(match) < 4 {
			continue
		}
		filename := match[1]
		dateStr := match[2]
		timeStr := match[3]

		// 1. соединение даты и времени
		fullDateTimeStr := fmt.Sprintf("%s %s", dateStr, timeStr)
		const layout = "02-Jan-2006 15:04" // аналог "%d-%b-%Y %H:%M"
		parsedTime, err := time.Parse(layout, fullDateTimeStr)
		if err != nil {
			g.logger.Debug("не удалось спарсить дату из репо MS, пропуск записи…", "date_string", fullDateTimeStr, "error", err)
			continue
		}

		// 2. извлечение имени версии
		version := strings.TrimPrefix(filename, "microsoft-edge-stable_")
		version = strings.TrimSuffix(version, "_amd64.deb")
		version = strings.TrimSuffix(version, "-1") // удаление суффикса "-1"

		releases = append(releases, msEdgeRelease{Version: version, Date: parsedTime})
	}

	if len(releases) == 0 {
		return nil, errors.New("не удалось спарсить ни одну валидную версию со страницы репозитория Microsoft Edge")
	}

	// сортировка по дате, чтобы самые свежие были в начале
	sort.Slice(releases, func(i, j int) bool {
		return releases[i].Date.After(releases[j].Date)
	})

	limit := min(versionsToKeepFromMS, len(releases))
	versions := make([]string, 0, limit)
	uniqueVersions := make(map[string]struct{})

	// удаление дубликатов
	for _, release := range releases {
		if _, exists := uniqueVersions[release.Version]; !exists {
			uniqueVersions[release.Version] = struct{}{}
			versions = append(versions, release.Version)
		}
		if len(versions) >= limit {
			break
		}
	}

	return versions, nil
}

// approximateVersionForDate вычисляет строку с одной версией для заданной даты.
func approximateVersionForDate(d time.Time) string {
	t0 := time.Date(2025, 5, 14, 0, 0, 0, 0, time.UTC)
	t := d.Sub(t0).Hours() / 24 // дней с момента t0

	M := 136 + (t / 31)

	knownBuild := map[int]float64{136: 7103, 137: 7151, 138: 7204, 139: 7258}
	B := 0.0
	if build, ok := knownBuild[int(M)]; ok {
		B = build
	} else {
		B = 7103 + 52*(M-136)
	}

	p := math.Round(0.88*t + 62.55)

	return fmt.Sprintf("%d.0.%d.%d", int(M), int(B), int(p))
}

// approximateVersions генерирует правдоподобный набор актуальных версий браузеров на текущую дату
func (g *Generator) approximateVersions() []string {
	versions := make([]string, 0, 5)
	// создание вариантов для сегодняшнего дня и недавнего прошлого для разнообразия
	for i := 0; i < 5; i++ {
		d := time.Now().AddDate(0, 0, -i*7) // сегодня, неделю назад, две недели назад…
		versions = append(versions, approximateVersionForDate(d))
	}
	return versions
}

// updateVersions пытается получить версии браузеров из сетевых источников параллельно до первого успеха или использует аппроксимацию.
func (g *Generator) updateVersions() error {
	// общий таймаут на все сетевые операции
	ctx, cancel := context.WithTimeout(context.Background(), g.httpClient.Timeout)
	defer cancel()

	resultsChan := make(chan []string, 2) // буферизированный канал для результатов
	var wg sync.WaitGroup
	wg.Add(2)

	// источник 1: Google API
	go func() {
		defer wg.Done()
		sourceName := "Google API"
		g.logger.Debug("попытка получить версии браузеров через Google API…")
		versions, err := g.fetchGoogleVersions(ctx)
		if err != nil {
			// --- ИЗМЕНЕНИЕ ЗДЕСЬ ---
			if errors.Is(err, context.Canceled) {
				g.logger.Debug("запрос к источнику был отменен, так как другой источник ответил быстрее", "source", sourceName)
			} else {
				g.logger.Warn("не удалось получить данные от источника", "source", sourceName, "error", err)
			}
			return
		}
		// неблокирующая отправка, если другой источник завершится успешно раньше
		select {
		case resultsChan <- versions:
			g.logger.Debug("получение версий браузеров через источник прошло успешно", "source", sourceName)
		case <-ctx.Done():
		}
	}()

	// источник 2: Microsoft Repo
	go func() {
		defer wg.Done()
		sourceName := "Microsoft Repo"
		g.logger.Debug("попытка получить версии браузеров из репозитория Microsoft…")
		versions, err := g.fetchMicrosoftVersions(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				g.logger.Debug("запрос к источнику был отменен, так как другой источник ответил быстрее", "source", sourceName)
			} else {
				g.logger.Warn("не удалось получить данные от источника", "source", sourceName, "error", err)
			}
			return
		}
		select {
		case resultsChan <- versions:
			g.logger.Debug("получение версий браузеров через источник прошло успешно", "source", sourceName)
		case <-ctx.Done():
		}
	}()

	// горутина для завершения обоих сетевых запросов
	allNetworkDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(allNetworkDone)
	}()

	// ожидание первого успешного запроса или завершения обоих
	select {
	case versions := <-resultsChan:
		g.logger.Info("версии браузеров успешно получены из сети!")
		g.mu.Lock()
		g.versions = versions
		g.mu.Unlock()
		return nil
	case <-allNetworkDone:
		// оба источника завершились безрезультатно
		g.logger.Warn("фоллбэк на аппроксимацию: сетевые источники версий браузеров завершились безрезультатно.")
		g.mu.Lock()
		g.versions = g.approximateVersions()
		g.mu.Unlock()
		return nil
	case <-ctx.Done():
		// общий таймаут
		g.logger.Error("фоллбэк на аппроксимацию: сетевые источники версий браузеров завершены по таймауту.")
		g.mu.Lock()
		g.versions = g.approximateVersions()
		g.mu.Unlock()
		return nil // фоллбэк всегда успешен, ошибки для возврата быть не может
	}
}

// GetVersions возвращает текущий набор версий браузеров
func (g *Generator) GetVersions() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	// создаем копию, чтобы избежать изменений извне
	versionsCopy := make([]string, len(g.versions))
	copy(versionsCopy, g.versions)
	return versionsCopy
}
