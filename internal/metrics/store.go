// Package metrics, Safe Zone dashboard'u için basit, bellek-içi (in-memory)
// istatistik ve son-olay (event) takibi sağlar.
//
// ÖNEMLİ TASARIM KARARI (bkz. issue #16 tartışması):
// Bu veriler kalıcı DEĞİLDİR -- servis yeniden başlatıldığında sıfırlanır.
// Bu, internal/middleware/ratelimit.go'daki rlState yapısıyla aynı
// felsefeyi izler: basit, bellekte tutulan, mutex korumalı sayaçlar.
// Kalıcı bir çözüm (örn. PostgreSQL tablosu) bilinçli olarak tercih
// edilmedi çünkü issue "no new complex services" istiyor ve bu, dashboard'un
// ilk (initial) sürümü için yeterli kabul edildi.
package metrics

import (
	"sync"
	"time"
)

// Event, dashboard'un Events ekranında gösterilecek tek bir /detect
// isteğinin ÖZET bilgisini temsil eder.
//
// GÜVENLİK NOTU: Bu struct'ta bilinçli olarak YOK olan alanlar:
//   - Tespit edilen gerçek PII değeri (örn. gerçek e-posta adresi)
//   - İsteğin ham metni (request body)
//
// Bu bilgiler /detect response'unda mevcut ama event kaydına dahil
// edilmiyor -- sadece "ne oldu" bilgisi tutuluyor, "ne içeriyordu" değil.
type Event struct {
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"request_id"`
	Blocked   bool      `json:"blocked"`
	Reason    string    `json:"reason"` // "PII", "GUARDRAIL", veya "RULE"
}

// Summary, Overview ekranındaki 4 sayaç kartına karşılık gelir.
type Summary struct {
	TotalRequests int64 `json:"total_requests"`
	Allowed       int64 `json:"allowed"`
	Blocked       int64 `json:"blocked"`
	PIIDetections int64 `json:"pii_detections"`
}

const maxEvents = 50

// store, tüm metrics state'ini tutan tekil (singleton) yapı.
// ratelimit.go'daki rlState ile aynı desen: paket-seviyesi bir
// değişken + mutex, ayrı bir "manager" struct'ı kurmuyoruz.
var store = struct {
	sync.Mutex
	summary Summary
	events  []Event // en yeni event, dizinin SONUNDA (append ile eklenir)
}{}

// RecordEvent, bir /detect isteği tamamlandığında çağrılır.
// Sayaçları günceller ve event'i ring buffer'a ekler.
func RecordEvent(e Event) {
	store.Lock()
	defer store.Unlock()

	store.summary.TotalRequests++
	if e.Blocked {
		store.summary.Blocked++
	} else {
		store.summary.Allowed++
	}
	if e.Reason == "PII" {
		store.summary.PIIDetections++
	}

	store.events = append(store.events, e)

	// Ring buffer davranışı: sınırı aşarsa en eski event'i at.
	// Sınırsız büyümesini engellemek için (bkz. senin promptundaki
	// "recent events için sınırsız query yapma" kuralı).
	if len(store.events) > maxEvents {
		store.events = store.events[len(store.events)-maxEvents:]
	}
}

// GetSummary, Overview ekranı için mevcut sayaçların bir kopyasını döner.
// Kopya döndürüyoruz ki çağıran taraf store'un iç durumunu doğrudan
// değiştiremesin (encapsulation).
func GetSummary() Summary {
	store.Lock()
	defer store.Unlock()
	return store.summary
}

// GetRecentEvents, en yeni event'lerden en fazla `limit` tanesini,
// en yeniden en eskiye sıralı olarak döner.
func GetRecentEvents(limit int) []Event {
	store.Lock()
	defer store.Unlock()

	if limit <= 0 || limit > len(store.events) {
		limit = len(store.events)
	}

	// store.events sonunda en yeni event var; sondan başlayıp
	// tersten limit kadar alıyoruz, sonra ters çeviriyoruz ki
	// response'ta "en yeni ilk sırada" olsun.
	result := make([]Event, limit)
	for i := 0; i < limit; i++ {
		result[i] = store.events[len(store.events)-1-i]
	}
	return result
}
