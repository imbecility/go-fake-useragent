// main.go (пример использования библиотеки go_fake_useragent)

package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/imbecility/go-fake-useragent/useragent"
)

func main() {
	// создаем логгер, чтобы видеть, что происходит внутри библиотеки
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// --- пример 1: простая инициализация ---
	// использует сетевые запросы или аппроксимацию.
	// без дискового кэша, будет делать запросы при каждом запуске программы.
	fmt.Println("--- простой UA-генератор ---")
	gen, err := useragent.NewGenerator(useragent.WithLogger(logger))
	if err != nil {
		// эта ошибка маловероятна из-за фоллбэка, но проверять нужно всегда.
		panic(err)
	}

	// генерируем несколько User-Agent
	for i := 0; i < 3; i++ {
		fmt.Println(gen.Get())
	}

	// --- пример 2: инициализация с дисковым кэшем ---
	// при первом запуске выполнит запросы и сохранит результат в /tmp/go_useragent_versions.json
	// при последующих запусках в течение часа будет мгновенно загружаться с диска.
	fmt.Println("\n--- UA-генератор с дисковым кешем ---")
	cachedGen, err := useragent.NewGenerator(
		useragent.WithLogger(logger),
		// кэшировать на диске в течение 1 часа, пустой путь использует директорию по умолчанию.
		useragent.WithDiskCache("", 1*time.Hour),
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf(" UA-генератор инициализирован с %d кэшированными версиями.\n", len(cachedGen.GetVersions()))

	for i := 0; i < 3; i++ {
		fmt.Println(cachedGen.Get())
	}

	// --- пример 3: генерация заголовков ---

	// 1. Генерация заголовков без указания URL
	fmt.Println("\n--- заголовки с fallback URL ---")
	headers1 := cachedGen.GetHeaders()
	for k, v := range headers1 {
		fmt.Printf("%s: %s\n", k, v)
	}

	// 2. Генерация заголовков для конкретного URL
	fmt.Println("\n--- заголовки для 'https://api.example.com/v1/data' ---")
	headers2 := cachedGen.GetHeaders("https://api.example.com/v1/data")
	for k, v := range headers2 {
		fmt.Printf("%s: %s\n", k, v)
	}

	// --- пример 4: генерация заголовков для ботов ---

	fmt.Println("\n--- заголовки для Googlebot ---")
	googleHeaders := gen.GetCrawlerHeaders(useragent.GoogleBot)
	for k, v := range googleHeaders {
		fmt.Printf("%s: %s\n", k, v)
	}

	fmt.Println("\n--- заголовки для BingBot ---")
	bingHeaders := gen.GetCrawlerHeaders(useragent.BingBot)
	for k, v := range bingHeaders {
		fmt.Printf("%s: %s\n", k, v)
	}

	fmt.Println("\n--- заголовки для YandexBot ---")
	yandexHeaders := gen.GetCrawlerHeaders(useragent.YandexBot)
	for k, v := range yandexHeaders {
		fmt.Printf("%s: %s\n", k, v)
	}
}
