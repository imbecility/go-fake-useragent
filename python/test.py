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

    print('\n--- явное закрытие (не обязательно) ---')
    ua.close()

    print('\n--- инициализация без кэша (будет использована аппроксимация, если нет сети)')
    print('и с контекстным менеджером `with` для автоматического управления ресурсами ---\n')

    with UserAgent(use_disk_cache=False) as ua:
        ua_no_cache = ua.get()
        print(f'UA без кэша: {ua_no_cache}')
        print(f'заголовки без кэша: {dumps(ua.get_headers("https://site.ru/url/path"), indent=2)}')



