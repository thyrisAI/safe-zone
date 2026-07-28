/**
 * Dashboard'da kullanacağımız GÜVENLİ validator şekli.
 *
 * ÖNEMLİ: Backend'in gerçek GET /validators response'unda bir
 * "rule" alanı vardır ve bu alan AI validator'ları için TAM,
 * uzun bir prompt metni içerir (örn. jailbreak tespiti nasıl
 * yapılıyor, hangi kelimelere bakılıyor -- hepsi açık metin).
 *
 * Bu bilgiyi dashboard'da GÖSTERMİYORUZ çünkü bir saldırgan bu
 * prompt'u okuyup sistemin tam olarak neye baktığını öğrenip
 * etrafından dolaşabilir (FAZ 6 ve FAZ 17 kararı).
 *
 * Bu yüzden burada "rule" alanı BİLİNÇLİ OLARAK YOK. Backend'den
 * gelen ham veriyi bu tipe çevirirken "rule" alanını atacağız
 * (api/validators.ts içinde, FAZ 14'te).
 */
export interface Validator {
    ID: number
    name: string
    type: 'BUILTIN' | 'REGEX' | 'SCHEMA' | 'AI_PROMPT'
    description: string
    expected_response: string
}
