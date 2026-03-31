package testhttp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
)

// Estructuras de datos para el escenario "Heavyweight"
type HeavyRecord struct {
	ID        int       `json:"id"`
	UUID      string    `json:"uuid"`
	Payload   string    `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
	Active    bool      `json:"active"`
}

type HeavyResponse struct {
	Total   int           `json:"total"`
	Data    []HeavyRecord `json:"data"`
	Version string        `json:"version"`
}

// SetupHeavyServer genera una respuesta JSON masiva de forma eficiente.
// 'count' define cuántos registros incluir en el slice 'Data'.
func SetupHeavyServer(count int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"total":%d,"version":"2.0","data":[`, count)
		for i := 0; i < count; i++ {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"id":%d,"uuid":"550e8400-e29b-41d4-a716-446655440000","payload":"lorem ipsum dolor sit amet consectetur adipiscing elit","timestamp":"2026-03-30T15:00:00Z","active":true}`, i)
		}
		fmt.Fprint(w, `]}`)
	}))
}

const (
	RecordsPerRequest = 50000 // 50k registros por petición (~7MB de JSON)
)

// --- ESCENARIO: ALTA CONCURRENCIA + BIG DATA ---

func BenchmarkHeavy_Parallel_Standard(b *testing.B) {
	server := SetupHeavyServer(RecordsPerRequest)
	defer server.Close()

	// Optimizamos el cliente estándar para alta concurrencia
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
			IdleConnTimeout:     90 * time.Second,
		},
		Timeout: 30 * time.Second,
	}

	b.ReportAllocs()
	b.ResetTimer()

	// RunParallel simula miles de peticiones concurrentes usando GOMAXPROCS goroutines
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			res, err := client.Get(server.URL)
			if err != nil {
				continue
			}

			var data HeavyResponse
			body, _ := io.ReadAll(res.Body)
			_ = json.Unmarshal(body, &data)

			res.Body.Close()

			// Forzamos un poco de trabajo para simular procesamiento real
			if data.Total != RecordsPerRequest {
				b.Errorf("data mismatch: expected %d, got %d", RecordsPerRequest, data.Total)
			}
		}
	})
}

func BenchmarkHeavy_Parallel_Relay(b *testing.B) {
	server := SetupHeavyServer(RecordsPerRequest)
	defer server.Close()

	// Relay permite configurar el pool de conexiones de forma sencilla
	relayClient := relay.New(
		relay.WithBaseURL(server.URL),
		relay.WithTimeout(30*time.Second),
		relay.WithConnectionPool(1000, 1000, 1000), // MaxIdle, MaxIdlePerHost, MaxPerHost
		relay.WithDisableRetry(),                   // Desactivado para medir overhead de procesamiento puro
	)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// ExecuteAs maneja internamente la lectura eficiente y el unmarshal genérico
			data, _, err := relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))
			if err != nil {
				continue
			}

			if data.Total != RecordsPerRequest {
				b.Errorf("data mismatch: expected %d, got %d", RecordsPerRequest, data.Total)
			}
		}
	})
}

// --- ESCENARIO: STRESS DE MEMORIA (GC PRESSURE) ---

func BenchmarkMemoryStress_Relay(b *testing.B) {
	server := SetupHeavyServer(100000) // 100k registros (~14MB de JSON)
	defer server.Close()

	relayClient := relay.New(relay.WithBaseURL(server.URL))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))

		// En escenarios pesados, el GC es el cuello de botella.
		// Analizamos cómo relay gestiona la memoria en ráfagas.
		if i%10 == 0 {
			runtime.GC()
		}
	}
}

// --- ESCENARIO: SMALL PAYLOADS CON ALTA CONCURRENCIA ---

func BenchmarkSmallPayload_Parallel_Relay(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"total":1,"version":"2.0","data":[{"id":1,"uuid":"550e8400-e29b-41d4-a716-446655440000","payload":"test","timestamp":"2026-03-30T15:00:00Z","active":true}]}`)
	}))
	defer server.Close()

	relayClient := relay.New(
		relay.WithBaseURL(server.URL),
		relay.WithConnectionPool(1000, 1000, 1000),
		relay.WithDisableRetry(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))
		}
	})
}

// --- ESCENARIO: STREAMING DE DATOS MASIVOS ---

func BenchmarkLargeStream_Sequential_Relay(b *testing.B) {
	// Simula un stream de 250k registros (~35MB de JSON)
	server := SetupHeavyServer(250000)
	defer server.Close()

	relayClient := relay.New(relay.WithBaseURL(server.URL))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))
	}
}

// --- ESCENARIO: CONEXIONES REUTILIZADAS EN LOOP ---

func BenchmarkConnectionReuse_Sequential_Relay(b *testing.B) {
	server := SetupHeavyServer(RecordsPerRequest)
	defer server.Close()

	relayClient := relay.New(
		relay.WithBaseURL(server.URL),
		relay.WithConnectionPool(1, 1, 100), // Single connection, reuse agresively
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))
	}
}

// --- ESCENARIO: COMPARACIÓN DE ALLOCATIONS (Relay vs net/http) ---

func BenchmarkAllocationProfile_Standard(b *testing.B) {
	server := SetupHeavyServer(RecordsPerRequest)
	defer server.Close()

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
		},
		Timeout: 30 * time.Second,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		res, _ := client.Get(server.URL)
		body, _ := io.ReadAll(res.Body)
		var data HeavyResponse
		_ = json.Unmarshal(body, &data)
		res.Body.Close()
	}
}

func BenchmarkAllocationProfile_Relay(b *testing.B) {
	server := SetupHeavyServer(RecordsPerRequest)
	defer server.Close()

	relayClient := relay.New(relay.WithBaseURL(server.URL))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))
	}
}

// --- ESCENARIO: IDLE CONNECTIONS CLEANUP ---

func BenchmarkIdleConnections_Relay(b *testing.B) {
	server := SetupHeavyServer(10000) // Smaller payload for this test
	defer server.Close()

	relayClient := relay.New(
		relay.WithBaseURL(server.URL),
		relay.WithConnectionPool(500, 500, 500),
	)

	b.ReportAllocs()
	b.ResetTimer()

	// Simulate pattern: burst of requests, then idle, then burst again
	for i := 0; i < b.N; i++ {
		_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))

		// Every 100 requests, simulate idle period
		if i%100 == 99 {
			time.Sleep(100 * time.Millisecond)
		}
	}
}

/*
NOTAS TÉCNICAS DE ALTO RENDIMIENTO:

1. Gestión de Memoria: 'relay.ExecuteAs' utiliza internamente un buffer optimizado
   para minimizar las re-asignaciones durante la lectura del Body. En escenarios
   de 50k+ registros, esto ayuda a mantener la fragmentación del montón bajo control.

2. Concurrencia: Al usar 'b.RunParallel', estamos probando la contención de mutex
   dentro del cliente. Relay brilla aquí al delegar la gestión del pool al motor
   de net/http pero envolviéndolo en una capa que evita fugas de descriptores de archivos.

3. Marshalling: El costo predominante en ambos casos es 'json.Unmarshal'. La ventaja
   de Relay es la ergonomía; permite manejar estos volúmenes con una fracción del
   código (LOC), reduciendo la superficie de errores en la gestión de recursos (cierre de body).

4. Pool de Conexiones: Relay optimiza internamente la reutilización de conexiones TCP
   reduciendo el handshake overhead en escenarios de alta concurrencia.

5. GC Pressure: Con pooling de buffers y estructura optimizada, Relay reduce significativamente
   el trabajo del garbage collector, crítico en aplicaciones de millones de req/s.
*/
