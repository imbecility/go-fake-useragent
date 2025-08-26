# go-fake-useragent

Библиотека Go для генерации актуальных и правдоподобных строк User-Agent и полных наборов HTTP-заголовков.

Также есть [Python-версия](https://github.com/imbecility/go-fake-useragent/tree/main/python).

В отличие от других подобных библиотек, `go-fake-useragent` не использует статичные, захардкоженные или обновляемые вручную списки юзер-агентов. Вместо этого она динамически получает последние версии браузеров (Chrome, Edge) из официальных источников (Google, Microsoft), обеспечивая постоянную актуальность генерируемых данных.

## Поддерживаемые браузеры

Только эти два десктопных браузера:

*   Google Chrome
*   Microsoft Edge

Потому что цель подобных библиотек - обеспечить маскировку под реальные массовые браузеры. Добавление остальных браузеров - бессмысленно, т.к. их доля в десктопном сегменте незначительная. Использование редких юзер-агентов - противоречит цели.

## Особенности

*   **Динамическое обновление:** версии браузеров загружаются из официальных API и репозиториев.
*   **Высокая отказоустойчивость** и многоуровневая система фоллбэка:
    1.  кэш на диске (опционально).
    2.  параллельные сетевые запросы к нескольким источникам (какой-нибудь да ответит!).
    3.  математическая аппроксимация версии на основе текущей даты как крайняя мера.
    4. `NewGenerator()` **принципиально** не может выбросить ошибку или столкнуться с исключением: цель не смотря ни на что выдать юзерагент через `Get()` или заголовки через `GetHeaders()` || `GetCrawlerHeaders`, даже если отвалиться сеть или жесткий диск.
*   **Генерация полных заголовков:** может генерировать не только `User-Agent`, но и соответствующие ему `sec-ch-ua` и прочие заголовки, имитируя реальный браузер (а уже в клиентском коде можно к ним добавить свои).
*   **Кэширование на диске:** ускоряет инициализацию при повторных запусках и снижает количество сетевых запросов.
*   **Поддержка поисковых ботов:** генерирует заголовки для маскировки под Googlebot, BingBot и YandexBot.
*   **Потокобезопасность:** безопасное использование в конкурентных приложениях.
*   **Нулевые зависимости:** используется только стандартная библиотека Go.
*   **Гибкая конфигурация:** позволяет использовать собственный `http.Client` и `slog.Logger`.

## Установка

```bash
go get github.com/imbecility/go-fake-useragent
```

## Быстрый старт

```go
package main

import (
	"fmt"
	"github.com/imbecility/go-fake-useragent/useragent"
	"log"
)

func main() {
	// Инициализация генератора.
	// При первом запуске выполнит сетевой запрос для получения актуальных версий.
	gen, err := useragent.NewGenerator()
	if err != nil {
		log.Fatal(err)
	}

	// Получение случайного User-Agent (Chrome или Edge)
	randomUA := gen.Get()
	fmt.Println(randomUA)
	// Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.7258.67 Safari/537.36
}
```

## Продвинутое использование

### Кэширование на диске

Чтобы избежать сетевых запросов при каждом запуске приложения, включите кэширование на диске.

```go
package main

import (
    "time"
    "github.com/imbecility/go-fake-useragent/useragent"
)

// Кэш будет храниться в системной директории временных файлов, и будет считаться актуальным в течение 24 часов.
cachedGen, err := useragent.NewGenerator(
    useragent.WithDiskCache("", 24*time.Hour),
)
if err != nil {
    log.Fatal(err)
}

// Этот вызов мгновенно загрузит версии с диска (если кэш не устарел).
fmt.Println(cachedGen.Get())
```

### Интеграция с логированием

Для отладки можно подключить логгер вашего приложения.

```go
package main

import (
    "log/slog"
    "os"
    "github.com/imbecility/go-fake-useragent/useragent"
)

logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

gen, err := useragent.NewGenerator(useragent.WithLogger(logger))
// Вывод в лог:
// time=... level=DEBUG msg="попытка получить версии браузеров через Google API…"
// time=... level=INFO msg="версии браузеров успешно получены из сети!"
```

## Генерация полных HTTP-заголовков

### Заголовки браузера

Для максимальной правдоподобности используйте `GetHeaders`. Этот метод генерирует полный набор заголовков, включая `sec-ch-ua`, на основе случайного User-Agent.

```go
// Генерация заголовков для запроса к api.example.com
headers := gen.GetHeaders("https://api.example.com/v1/data")

for key, value := range headers {
    fmt.Printf("%s: %s\n", key, value)
}

/* Примерный вывод:
   user-agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.7204.168 Safari/537.36 Edg/138.0.7204.168
   accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*//*;q=0.8,application/signed-exchange;v=b3;q=0.7
   origin: https://api.example.com
   referer: https://api.example.com
   sec-ch-ua: "Not A Brand";v="99", "Chromium";v="138", "Microsoft Edge";v="138"
   ... и другие
*/
```

### Заголовки поисковых ботов

```go
// Генерация заголовков для Googlebot
googleBotHeaders := gen.GetCrawlerHeaders(useragent.GoogleBot)
fmt.Println(googleBotHeaders["user-agent"])
// Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/139.0.7258.128 Safari/537.36

// Генерация заголовков для YandexBot
yandexBotHeaders := gen.GetCrawlerHeaders(useragent.YandexBot)
fmt.Println(yandexBotHeaders["user-agent"])
// Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)
```
**Важно:** Продвинутые системы защиты проверяют не только `User-Agent`, но и IP-адрес запроса с помощью rDNS. Для успешной имитации бота запрос должен исходить из подсети, принадлежащей поисковой системе (Google Colab, Google Cloud).


---

Более подробный пример использования в файле [main.go](https://github.com/imbecility/go-fake-useragent/blob/main/main.go).

