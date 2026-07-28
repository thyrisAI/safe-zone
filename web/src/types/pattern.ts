/**
 * Safe Zone backend'inin GET /patterns endpoint'inden döndüğü
 * ham (raw) pattern şekli.
 *
 * Not: Alan isimleri backend'deki gerçek JSON response'a göre
 * PascalCase'dir (GORM'un varsayılan serialize davranışı).
 * Bu, Validator tipinden farklıdır -- backend tutarsızlığını
 * burada olduğu gibi yansıtıyoruz, uydurmuyoruz.
 */
export interface Pattern {
    ID: number
    Name: string
    Regex: string
    Description: string
    Category: 'PII' | 'SECRET' | 'INJECTION'
    IsActive: boolean
    BlockThreshold: number | null
    AllowThreshold: number | null
  }