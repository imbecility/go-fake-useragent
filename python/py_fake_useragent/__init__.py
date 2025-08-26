import atexit
import ctypes
import json
import sys
from enum import IntEnum
from logging import Logger
from pathlib import Path
from typing import Dict, Optional
from warnings import warn

from .exceptions import (
    BufferTooSmallError, InitializationError, JsonMarshalError, LibraryLoadError,
    NotInitializedError, UnknownCrawlerTypeError, UserAgentException
)

LOG_CALLBACK_TYPE = ctypes.CFUNCTYPE(None, ctypes.c_char_p)
_GLOBAL_LIB: Optional[ctypes.CDLL] = None


class CrawlerType(IntEnum):
    GOOGLE = 0
    BING = 1
    YANDEX = 2


class UserAgent:
    """
    библиотека для генерации User-Agent и HTTP-заголовков.

    - `get` - генерация случайных User-Agent строк
    - `get_headers` - получение HTTP-заголовков
    - `get_crawler_headers` - получение заголовков поисковых роботов

    Args:
        use_disk_cache (bool): Использовать ли дисковый кэш для хранения данных.
        cache_ttl_days (int): The second parameter.
        logger (Logger): Опциональный экземпляр Logger для приёма логов из Go-библиотеки.

    Raises:
        LibraryLoadError: библиотека не найдена или не удалось загрузить
        InitializationError: инициализация библиотеки завершилась с ошибкой
    """

    def __init__(self, use_disk_cache: bool = True, cache_ttl_days: int = 1, logger: Optional[Logger] = None):
        self._lib = self._load_library()
        self._define_signatures()
        self._is_closed = False
        self._c_log_callback = None

        if logger:
            self._setup_logging(logger)

        result = self._lib.Initialize(use_disk_cache, cache_ttl_days)
        if result != 0:
            self._handle_error_code(result)

    def _load_library(self) -> ctypes.CDLL:
        """
        загружает динамическую библиотеку в память

        Returns:
            загруженный объект ctypes.CDLL

        Raises:
            LibraryLoadError: если файл библиотеки не найден или не удалось загрузить
        """

        global _GLOBAL_LIB
        if _GLOBAL_LIB: return _GLOBAL_LIB

        lib_dir = Path(__file__).parent / 'lib'
        lib_name = {'win32': 'useragent.pyd', 'darwin': 'useragent.dylib'}.get(sys.platform, 'useragent.so')
        lib_path = lib_dir / lib_name

        if not lib_path.exists(): raise LibraryLoadError(f'файл библиотеки не найден: {lib_path}')
        try:
            _GLOBAL_LIB = ctypes.CDLL(str(lib_path))
            atexit.register(self._global_shutdown)  # регистрация глобального завершения библиотеки при выходе
            return _GLOBAL_LIB
        except OSError as e:
            raise LibraryLoadError(f'ошибка при загрузке библиотеки {lib_path}: {e}')

    @staticmethod
    def _global_shutdown():
        """Вызывается при выходе из Python для корректного завершения работы Go-рантайма."""
        if _GLOBAL_LIB:
            _GLOBAL_LIB.Shutdown()

    def _define_signatures(self):
        """
        определяет сигнатуры (argtypes/restype) для функций библиотеки,
        чтобы ctypes мог корректно передавать и получать данные.
        """
        self._lib.Initialize.argtypes = [ctypes.c_bool, ctypes.c_int]
        self._lib.Initialize.restype = ctypes.c_int
        self._lib.Shutdown.restype = None
        self._lib.SetLoggerCallback.argtypes = [ctypes.c_void_p]
        self._lib.GetDroppedLogs.restype = ctypes.c_ulonglong

        # словарь с описанием аргументов для функций, возвращающих данные в буфер
        arg_types = {
            'GetRandomUA': [ctypes.c_void_p, ctypes.c_size_t],
            'GetHeaders': [ctypes.c_char_p, ctypes.c_void_p, ctypes.c_size_t],
            'GetCrawlerHeaders': [ctypes.c_int, ctypes.c_void_p, ctypes.c_size_t],
        }
        for name, types in arg_types.items():
            func = getattr(self._lib, name)
            func.argtypes = types
            func.restype = ctypes.c_int

    def _setup_logging(self, logger: Logger):
        """
        настраивает callback-функцию для приёма логов из библиотеки и перенаправления их в Python-логгер

        Args:
            экземпляр Logger
        """

        def py_log_callback(message_ptr: bytes):
            try:
                message = message_ptr.decode("utf-8", errors="ignore").strip()
                if 'level=DEBUG' in message:
                    logger.debug(message)
                elif 'level=INFO' in message:
                    logger.info(message)
                elif 'level=WARN' in message:
                    logger.warning(message)
                elif 'level=ERROR' in message:
                    logger.error(message)
                else:
                    logger.info(message)
            except Exception as e:
                print(f'ФАТАЛЬНАЯ ОШИБКА в Python log callback: {e}', file=sys.stderr)

        self._c_log_callback = LOG_CALLBACK_TYPE(py_log_callback)
        self._lib.SetLoggerCallback(ctypes.cast(self._c_log_callback, ctypes.c_void_p))

    @staticmethod
    def _handle_error_code(code: int):
        """
        маппинг кодов ошибки библиотеки и поднятие исключения
        Args:
            code (int): код ошибки

        Raises: UserAgentException

        """
        error_map = {-1: NotInitializedError, -2: JsonMarshalError, -3: UnknownCrawlerTypeError,
                     -4: InitializationError}
        exc_class = error_map.get(code, UserAgentException)
        raise exc_class(f'внутренняя ошибка внешней библиотеки, код: {code}')

    def _call_go_with_buffer(self, go_func, *args, initial_size=256) -> str:
        """
        универсальный метод для вызова функций библиотеки, которые возвращают данные в буфер

        алгоритм: выделение буфера начального размера -> вызов внешней функции ->
        коррекция размера буфера при необходимости с повтором вызова -> возврат строки

        Args:
            go_func: указатель на функцию из внешней библиотеки
            args: аргументы для функции
            initial_size: начальный размер буфера
        Returns:
            str декодированная строка с результатом

        Raises:
            BufferTooSmallError: если даже после повторного вызова буфера не хватило
            RuntimeError при попытке вызова если UserAgent уже закрыт
        """
        if self._is_closed: raise RuntimeError('экземпляр UserAgent уже закрыт!')

        buffer = ctypes.create_string_buffer(initial_size)
        result = go_func(*args, ctypes.c_void_p(ctypes.addressof(buffer)), initial_size)

        if 0 <= result < initial_size:
            return buffer.value.decode('utf-8')
        if result < 0: self._handle_error_code(result)

        required_size = result
        buffer = ctypes.create_string_buffer(required_size)
        new_result = go_func(*args, ctypes.c_void_p(ctypes.addressof(buffer)), required_size)
        if 0 <= new_result < required_size: return buffer.value.decode('utf-8')
        if new_result < 0: self._handle_error_code(new_result)

        raise BufferTooSmallError(
            f"Не удалось получить данные из Go. Требуемый размер: {required_size}, код: {new_result}")

    def get(self) -> str:
        """
        генерирует случайный User-Agent для актуальных десктопных Chrome/Edge

        Returns:
            str строка User-Agent
        """
        return self._call_go_with_buffer(self._lib.GetRandomUA)

    def get_headers(self, url: str = '') -> dict[str, str]:
        """
        генерирует полноценные заголовки браузеров Chrome/Edge

        опционально можно указать ссылку (по которой планируется делать запрос) чтобы добавить referer/origin заголовки

        Args:
            url (str): целевая ссылка для последующего запроса

        Returns:
            dict словарь с заголовками браузера
        """
        json_str = self._call_go_with_buffer(self._lib.GetHeaders, url.encode('utf-8'), initial_size=2048)
        return json.loads(json_str)

    def get_crawler_headers(self, crawler: CrawlerType) -> Dict[str, str]:
        """
        генерирует заголовки поисковых роботов

        Args:
            crawler (CrawlerType): тип поискового робота (GOOGLE, BING, YANDEX)

        Returns:
            dict словарь с заголовками краулера
        """
        json_str = self._call_go_with_buffer(self._lib.GetCrawlerHeaders, crawler.value, initial_size=1024)
        return json.loads(json_str)

    def get_dropped_logs(self) -> int:
        """
        возвращает количество отброшенных сообщений лога из-за переполнения буфера
        """
        return self._lib.GetDroppedLogs()

    def close(self):
        """
        закрывает экземпляр, останавливает обмен логами и высвобождает ресурсы
        """
        if not self._is_closed:
            if self._c_log_callback:
                self._lib.SetLoggerCallback(None)
                self._c_log_callback = None
            self._is_closed = True

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()

    def __del__(self):
        try:
            if sys.is_finalizing(): return
        except (ImportError, AttributeError):
            pass
        if not self._is_closed:
            warn('экземпляр UserAgent не был явно закрыт! используй контекстный менеджер `with` или вызывай `.close()`',
                 ResourceWarning)
            self.close()
