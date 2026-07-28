/**
 * Ortak API client. Tüm backend isteklerinin geçtiği tek nokta.
 *
 * Neden burada topluyoruz?
 * - Base URL, header, hata yönetimi gibi ortak mantığı tek yerde tutmak için.
 * - Bir endpoint şekli değişirse sadece burada veya ilgili api/*.ts
 *   dosyasında düzeltme yaparız, sayfalara (pages/) dokunmayız.
 */

// Vite proxy ayarımız sayesinde burası hep "/api" -- gerçek backend adresi
// (localhost:8080) hiçbir zaman frontend kodunda görünmüyor.
const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '/api'

// Bir isteğin çok uzun sürmesini engellemek için varsayılan zaman aşımı.
const DEFAULT_TIMEOUT_MS = 8000

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

/**
 * JSON döndüren endpoint'ler için (örn. /patterns, /validators).
 * T: beklenen response tipini çağıran taraf belirtir, örn. request<Pattern[]>('/patterns')
 */
export async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), DEFAULT_TIMEOUT_MS)

  try {
    const response = await fetch(`${BASE_URL}${path}`, {
      ...options,
      signal: controller.signal,
      headers: {
        'Content-Type': 'application/json',
        ...options?.headers,
      },
    })

    if (!response.ok) {
      // Backend'in ham hata metnini kullanıcıya olduğu gibi göstermiyoruz
      // (senin kuralın: "raw backend stack trace gösterme").
      throw new ApiError(`Request failed with status ${response.status}`, response.status)
    }

    return (await response.json()) as T
  } catch (err) {
    if (err instanceof ApiError) throw err
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw new ApiError('Request timed out', 0)
    }
    throw new ApiError('Network error, please check your connection', 0)
  } finally {
    clearTimeout(timeoutId)
  }
}

/**
 * Düz metin döndüren endpoint'ler için (örn. /healthz -> "UP", /ready -> "READY").
 * JSON parse etmeye çalışmıyoruz çünkü bu endpoint'ler JSON döndürmüyor.
 */
export async function requestText(path: string): Promise<{ text: string; ok: boolean }> {
  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), DEFAULT_TIMEOUT_MS)

  try {
    const response = await fetch(`${BASE_URL}${path}`, { signal: controller.signal })
    const text = await response.text()
    return { text, ok: response.ok }
  } catch {
    // Health check'lerde hata fırlatmak yerine "ulaşılamadı" bilgisini
    // döndürüyoruz -- Overview ekranı bunu "Unreachable" olarak gösterecek.
    return { text: '', ok: false }
  } finally {
    clearTimeout(timeoutId)
  }
}