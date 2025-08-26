[GitHub: py-fake-useragent](https://github.com/imbecility/go-fake-useragent/tree/main/python)

Библиотека Python+Go для генерации актуальных и правдоподобных строк User-Agent и полных наборов HTTP-заголовков.
py-fake-useragent - это скомпилированная библиотека [go-fake-useragent](https://github.com/imbecility/go-fake-useragent) для Python через [cgo](https://pkg.go.dev/cmd/cgo) чтобы не переписывать одну и туже логику. Работает через [ctypes](https://docs.python.org/3/c-api/init.html).

В отличие от других подобных библиотек, `py-fake-useragent` не использует статичные, захардкоженные или обновляемые вручную списки юзер-агентов. Вместо этого она динамически получает последние версии браузеров (Chrome, Edge) из официальных источников (Google, Microsoft), обеспечивая постоянную актуальность генерируемых данных.

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
*   **Генерация полных заголовков:** может генерировать не только `User-Agent`, но и соответствующие ему `sec-ch-ua` и прочие заголовки, имитируя реальный браузер (а уже в клиентском коде можно к ним добавить свои).
*   **Кэширование на диске:** ускоряет инициализацию при повторных запусках и снижает количество сетевых запросов.
*   **Поддержка поисковых ботов:** генерирует заголовки для маскировки под Googlebot, BingBot и YandexBot.
*   **Нулевые зависимости:** используется только стандартная библиотека Go.


## Установка

```bash
uv add py_fake_useragent
````

или:

```bash
pip install py_fake_useragent
````

## Использование

```python
import logging
from json import dumps

from py_fake_useragent import UserAgent, CrawlerType

# настройка стандартного логгера Python,
# для большей скорости лучше использовать: https://github.com/microsoft/picologging
logging.basicConfig(level=logging.DEBUG, format='[%(levelname)s] %(message)s')
py_logger = logging.getLogger('GoUserAgent')

if __name__ == '__main__':
    print('--- инициализация с кэшем и логгером ---')
    ua = UserAgent(use_disk_cache=True, logger=py_logger)

    print('\n--- получение User-Agent ---')
    random_ua = ua.get()
    print(f'Случайный UA: {random_ua}')

    print('\n--- получение заголовков ---')
    headers = ua.get_headers('https://example.com/path')
    print(dumps(headers, indent=2))

    print('\n--- получение заголовков краулера ---')
    google_headers = ua.get_crawler_headers(CrawlerType.GOOGLE)
    print('Google Bot:\n', dumps(google_headers, indent=2))

    print('\n--- явное закрытие (не обязательно, но рекомендуется если библиотека не будет использоваться дальше) ---')
    ua.close()

    print('\n--- инициализация без кэша (будет использована аппроксимация, если нет сети)')
    print('и с контекстным менеджером `with` для автоматического управления ресурсами ---\n')

    with UserAgent(use_disk_cache=False) as ua:
        ua_no_cache = ua.get()
        print(f'UA без кэша: {ua_no_cache}')
        print(f'заголовки без кэша: {dumps(ua.get_headers("https://site.ru/url/path"), indent=2)}')

```

**выходные данные:**

```
--- инициализация с кэшем и логгером ---
[DEBUG] time=2025-08-26T14:15:22.168+05:00 level=DEBUG msg="успешно загружены версии User-Agent из кэша на диске"

--- получение User-Agent ---
Случайный UA: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.7258.67 Safari/537.36

--- получение заголовков ---
{
  "accept": "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
  "accept-language": "en-US,en;q=0.9",
  "connection": "keep-alive",
  "origin": "https://example.com",
  "priority": "u=0, i",
  "referer": "https://example.com",
  "sec-ch-ua": "\"Not;A_Brand\";v=\"99\", \"Chromium\";v=\"139\", \"Google Chrome\";v=\"139\"",
  "sec-ch-ua-arch": "\"x86\"",
  "sec-ch-ua-bitness": "\"64\"",
  "sec-ch-ua-form-factors": "\"Desktop\"",
  "sec-ch-ua-full-version": "\"139.0.7258.138\"",
  "sec-ch-ua-mobile": "?0",
  "sec-ch-ua-model": "\"\"",
  "sec-ch-ua-platform": "\"Windows\"",
  "sec-ch-ua-platform-version": "\"19.0.0\"",
  "sec-ch-viewport-height": "\"676\"",
  "sec-ch-viewport-width": "\"1472\"",
  "sec-fetch-dest": "document",
  "sec-fetch-mode": "navigate",
  "sec-fetch-site": "same-origin",
  "sec-fetch-user": "?1",
  "upgrade-insecure-requests": "1",
  "user-agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.7258.138 Safari/537.36"
}

--- получение заголовков краулера ---
Google Bot:
 {
  "accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
  "accept-encoding": "gzip, deflate, br",
  "accept-language": "en-US,en;q=0.9",
  "from": "googlebot(at)google.com",
  "user-agent": "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/139.0.7258.139 Safari/537.36"
}

--- явное закрытие (не обязательно) ---

--- инициализация без кэша (будет использована аппроксимация, если нет сети)
и с контекстным менеджером `with` для автоматического управления ресурсами ---

UA без кэша: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.7204.169 Safari/537.36
заголовки без кэша: {
  "accept": "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
  "accept-language": "en-US,en;q=0.9",
  "connection": "keep-alive",
  "origin": "https://site.ru",
  "priority": "u=0, i",
  "referer": "https://site.ru",
  "sec-ch-ua": "\"Not-A/Brand\";v=\"8\", \"Chromium\";v=\"138\", \"Google Chrome\";v=\"138\"",
  "sec-ch-ua-arch": "\"x86\"",
  "sec-ch-ua-bitness": "\"64\"",
  "sec-ch-ua-form-factors": "\"Desktop\"",
  "sec-ch-ua-full-version": "\"138.0.7204.170\"",
  "sec-ch-ua-mobile": "?0",
  "sec-ch-ua-model": "\"\"",
  "sec-ch-ua-platform": "\"Windows\"",
  "sec-ch-ua-platform-version": "\"19.0.0\"",
  "sec-ch-viewport-height": "\"712\"",
  "sec-ch-viewport-width": "\"1312\"",
  "sec-fetch-dest": "document",
  "sec-fetch-mode": "navigate",
  "sec-fetch-site": "same-origin",
  "sec-fetch-user": "?1",
  "upgrade-insecure-requests": "1",
  "user-agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.7204.170 Safari/537.36"
}
```