// ./cmd/c-wrapper/main.go

// пакет экспортирует API go-fake-useragent в С (.so/.dll/.dylib) через CGo для Python ctypes.
// управляет состоянием потокобезопасным синглтоном sync.Once (инициализация/очистка ресурсов).
// логирует асинхронно через C-коллбэк в Python с буферизацией.
// обеспечивает межъязыковой обмен/маршалинг данных и управление памятью Python.
package main

/*
#include <stdlib.h>
#include <stdbool.h>
#include <stddef.h>

// тип C коллбэка для передачи логов из Go в Python: принимает строку (char*) и ничего не возвращает
typedef void (*log_callback_f)(char*);

// статическая inline-функция для безопасного вызова коллбэка через проверку NULL в С
// (проще, чем обработка нулевого указателя в Go)
static inline void call_log_callback(log_callback_f f, char* msg) {
   if (f != NULL) {
	   f(msg);
   }
}
*/
import "C"

import (
	"bytes"
	"context"
	"encoding/json"
	ua "github.com/imbecility/go-fake-useragent/useragent"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// функция main необходима для пакета но пуста:
// библиотека использует экспортируемые функции
// с директивой //export как точку входа вместо main
func main() {}

// коды возврата с API (отрицательные - для ошибок(
const (
	ErrSuccess        = 0  // успех
	ErrNotInitialized = -1 // библиотека не была инициализирована вызовом Initialize
	ErrJSONMarshal    = -2 // ошибка сериализации данных в JSON
	ErrUnknownCrawler = -3 // передан неизвестный тип поискового робота
	ErrInitialization = -4 // ошибка инициализации
)

var (
	globalGenerator *ua.Generator // единый глобальный синглтон генератора user-agent'ов
	initErr         error         // хранит ошибку, возникшую во время инициализации
	initOnce        sync.Once     // гарантия однократной инициализации, даже при конкурентных вызовах из разных потоков Python
	shutdownOnce    sync.Once     // гарантия однократного завершения

	// --- асинхронное python-логгирование ---

	cLogCallback    C.log_callback_f // защищеный мьютексом указатель на функцию логирования для Python
	logMutex        sync.Mutex       // защита от гонок для cLogCallback
	logChannel      chan string      // буферизованный канал aсинхронной передачи логов из между Go и Python
	stopLogs        chan struct{}    // канал для сигнала о завершении работы горутины-обработчика логов
	processorWG     sync.WaitGroup   // ожидание остановки обработчика логов
	droppedLogs     atomic.Uint64    // счетчик отброшенных логов (при переполнении)
	loggingDisabled atomic.Bool      // флаг быстрого отключения логирования при завершении, когда канал уже закрыт
)

// PythonLogHandler перенаправляет логи из Go в Python коллбэк через slog.Handler
type PythonLogHandler struct {
	opts    slog.HandlerOptions
	bufPool *sync.Pool // пул буферов для уменьшения аллокаций памяти
	attrs   []slog.Attr
	group   string
}

// NewPythonLogHandler создает новый экземпляр обработчика логов
func NewPythonLogHandler() *PythonLogHandler {
	return &PythonLogHandler{
		opts:    slog.HandlerOptions{Level: slog.LevelDebug},
		bufPool: &sync.Pool{New: func() any { return new(bytes.Buffer) }},
	}
}

// Enabled проверка необходимости обработки лога данного уровня
func (h *PythonLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	// проверяет уровень и отключает логирование при завершении во избежание паники
	return !loggingDisabled.Load() && level >= h.opts.Level.Level()
}

// Handle форматирует запись лога и отправляет ее в асинхронный канал
func (h *PythonLogHandler) Handle(ctx context.Context, r slog.Record) error {
	if loggingDisabled.Load() {
		return nil
	}
	buf := h.bufPool.Get().(*bytes.Buffer) // пул буферов, чтобы снизить нагрузку на сборщик мусора
	buf.Reset()
	defer h.bufPool.Put(buf)

	// временный текстовый обработчик для форматирования записи
	var tempHandler slog.Handler = slog.NewTextHandler(buf, &h.opts)

	// применение атрибутов и групп (если есть)
	if len(h.attrs) > 0 {
		tempHandler = tempHandler.WithAttrs(h.attrs)
	}
	if h.group != "" {
		tempHandler = tempHandler.WithGroup(h.group)
	}

	// форматирование записи в буфер
	if err := tempHandler.Handle(ctx, r); err != nil {
		return err
	}

	// неблокирующая отправка в канал
	select {
	case logChannel <- buf.String(): // сообщение отправлено
	default:
		// при переполнении сообщение отбрасывается чтобы не блокировать горутину, счетчик увеличивается
		droppedLogs.Add(1)
	}
	return nil
}

// WithAttrs возвращает новый обработчик с добавленными атрибутами
func (h *PythonLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandler := *h
	newHandler.attrs = make([]slog.Attr, len(h.attrs), len(h.attrs)+len(attrs))
	copy(newHandler.attrs, h.attrs)
	newHandler.attrs = append(newHandler.attrs, attrs...)
	return &newHandler
}

// WithGroup возвращает новый обработчик внутри именованной группы
func (h *PythonLogHandler) WithGroup(name string) slog.Handler {
	newHandler := *h
	if newHandler.group != "" {
		newHandler.group += "." + name
	} else {
		newHandler.group = name
	}
	return &newHandler
}

// startLogProcessor запускает фоновую горутину для чтения из logChannel и передачи сообщений в C-коллбэк
func startLogProcessor() {
	stopLogs = make(chan struct{})
	processorWG.Add(1)
	go func() {
		defer processorWG.Done()
		for {
			select {
			case msg, ok := <-logChannel:
				if !ok {
					// Канал был закрыт, но в этой логике мы используем stopLogs. Эта ветка на случай непредвиденного закрытия.
					return
				}
				// потокобезопасное получение текущего коллбэка
				logMutex.Lock()
				callback := cLogCallback
				logMutex.Unlock()
				if callback == nil {
					// пропуск сообщения, если коллбэк не установлен
					continue
				}
				// преобразование строки сообщения из Go в C-строку с аллокацией памяти в C
				cMsg := C.CString(msg)
				// вызов C-обертки для передачи сообщения
				C.call_log_callback(callback, cMsg)
				// освобождение памяти, выделенной под C.CString
				C.free(unsafe.Pointer(cMsg))
			// получен сигнал завершения
			case <-stopLogs:
				return
			}
		}
	}()
}

// Initialize экспортируется в C, однократно инициализирует глобальный генератор user-agent'ов
//
// потокобезопасна благодаря `sync.Once`
//
// параметры:
//   - useCache: C.bool (true), если нужно использовать дисковый кеш
//   - cacheTTLDays: C.int, время жизни (TTL) кеша в днях
//
// возвращает:
//   - C.int: 0 (ErrSuccess) в случае успеха, или код ошибки
//
//export Initialize
func Initialize(useCache C.bool, cacheTTLDays C.int) C.int {
	initOnce.Do(func() {
		// инициализация системы асинхронного логирования
		logChannel = make(chan string, 100) // буфер на 100 сообщений.
		startLogProcessor()
		logger := slog.New(NewPythonLogHandler())
		opts := []ua.Option{ua.WithLogger(logger)}
		if bool(useCache) {
			opts = append(opts, ua.WithDiskCache("", time.Duration(cacheTTLDays)*24*time.Hour))
		}
		globalGenerator, initErr = ua.NewGenerator(opts...)
	})
	if initErr != nil {
		return C.int(ErrInitialization)
	}
	return C.int(ErrSuccess)
}

// Shutdown экспортируется в C, корректно завершает работу библиотеки,
// отключает логирование и дожидается завершения обработки всех оставшихся логов,
// потокобезопасна благодаря `sync.Once`.
//
//export Shutdown
func Shutdown() {
	shutdownOnce.Do(func() {
		// установка атомарного флана, чтобы новые логи больше не обрабатывались
		loggingDisabled.Store(true)
		logMutex.Lock()
		cLogCallback = nil
		logMutex.Unlock()
		// сигнал горутине-обработчику логов о завершении
		if stopLogs != nil {
			close(stopLogs)
		}

		// ожидание завершения горутины-обработчика
		processorWG.Wait()
	})
}

// SetLoggerCallback экспортируется в C, устанавливает функцию обратного вызова для получения логов.
//
// параметры:
//   - callback: указатель на функцию типа `log_callback_f` в Python.
//     NULL - отключает
//
//export SetLoggerCallback
func SetLoggerCallback(callback C.log_callback_f) {
	logMutex.Lock()
	defer logMutex.Unlock()
	cLogCallback = callback
}

// GetDroppedLogs экспортируется в C, возвращает количество отброшенных из-за переполнения сообщений лога
//
// возвращает:
//   - C.ulonglong: количество отброшенных логов
//
//export GetDroppedLogs
func GetDroppedLogs() C.ulonglong {
	return C.ulonglong(droppedLogs.Load())
}

// copyToBuffer внутренняя функция-хелпер для безопасного копирования данных из Go-слайса байт в C-буфер,
// предоставленный вызывающей стороной Python, через паттерн двойного вызова:
//   - первый вызов с NULL-буфером возвращает требуемый размер
//   - python выделяет буфер нужного размера
//   - второй вызов с правильным буфером и размером выполняет копирование
//
// параметры:
//   - data: Go-слайс с данными для копирования
//   - buffer: указатель на C-буфер
//   - length: размер C-буфера
//
// возвращает:
//   - C.int:
//   - при length меньше требуемого - размер буфера с нуль-терминатором
//   - при успехе - количество скопированных байт без нуль-терминатора
func copyToBuffer(data []byte, buffer *C.char, length C.size_t) C.int {
	// значение для 32-битного знакового целого, 2147483647 - безопасный предел для C.int на большинстве платформ
	const maxCInt = math.MaxInt32

	// 1. проверка на превышения требуемого размера для C.int
	// len(data) возвращает int, который на 64-битных системах может быть больше MaxInt32.
	requiredSize := len(data) + 1
	if requiredSize > maxCInt {
		// требуемый размер не поместится в C.int, возврат макс. значения как сигнала о невозможности выполнения
		return C.int(maxCInt)
	}

	// 2. проверка размера буфера
	if uint64(length) < uint64(requiredSize) {
		return C.int(requiredSize) // requiredSize точно помещается в C.int
	}

	// 3. безопасное создание Go-слайса
	// приведение int(length) здесь безопасно, так как Python не выделит буфер на 2ГБ
	destinationSlice := unsafe.Slice((*byte)(unsafe.Pointer(buffer)), int(length))

	// 4. копирование данных и добавление нуль-терминатора
	bytesCopied := copy(destinationSlice, data)
	destinationSlice[bytesCopied] = 0

	// 5. возврат количества скопированных байт
	return C.int(bytesCopied)
}

// GetRandomUA экспортируется в C, генерирует случайный User-Agent
// использует паттерн двойного вызова с помощью copyToBuffer
//
// параметры:
//   - buffer: указатель на буфер для записи строки User-Agent
//   - length: размер буфера.
//
// возвращает:
//   - C.int: код ошибки, требуемый размер буфера или количество скопированных байт
//
//export GetRandomUA
func GetRandomUA(buffer *C.char, length C.size_t) C.int {
	if globalGenerator == nil {
		return C.int(ErrNotInitialized)
	}
	return copyToBuffer([]byte(globalGenerator.Get()), buffer, length)
}

// GetHeaders экспортируется в C, генерирует заголовки для HTTP-запроса, возвращает их в виде JSON-строки
//
// параметры:
//   - url: URL, для которого генерируются заголовки или NULL
//   - buffer: указатель на буфер для записи JSON-строки
//   - length: размер буфера
//
// возвращает:
//   - C.int: код ошибки, требуемый размер буфера или количество скопированных байт.
//
//export GetHeaders
func GetHeaders(url *C.char, buffer *C.char, length C.size_t) C.int {
	if globalGenerator == nil {
		return C.int(ErrNotInitialized)
	}
	var goURL string
	if url != nil {
		goURL = C.GoString(url)
	}

	headersMap := globalGenerator.GetHeaders(goURL)
	jsonData, err := json.Marshal(headersMap)
	if err != nil {
		return C.int(ErrJSONMarshal)
	}
	return copyToBuffer(jsonData, buffer, length)
}

// GetCrawlerHeaders экспортируется в C, генерирует заголовки под поискового робота, возвращает их в виде JSON-строки
//
// параметры:
//   - crawlerType: тип робота (0: google, 1: bing, 2: yandex)
//   - buffer: указатель на буфер для записи JSON-строки
//   - length: размер буфера
//
// возвращает:
//   - C.int: код ошибки, требуемый размер буфера или количество скопированных байт
//
//export GetCrawlerHeaders
func GetCrawlerHeaders(crawlerType C.int, buffer *C.char, length C.size_t) C.int {
	if globalGenerator == nil {
		return C.int(ErrNotInitialized)
	}
	// проверка что тип краулера 0-2
	if crawlerType < 0 || crawlerType > 2 {
		return C.int(ErrUnknownCrawler)
	}

	headersMap := globalGenerator.GetCrawlerHeaders(ua.CrawlerType(crawlerType))
	jsonData, err := json.Marshal(headersMap)
	if err != nil {
		return C.int(ErrJSONMarshal)
	}
	return copyToBuffer(jsonData, buffer, length)
}
