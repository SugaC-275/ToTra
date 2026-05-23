package middleware

import (
	"regexp"
	"time"

	"github.com/gofiber/fiber/v2"
)

type piiRule struct {
	name string
	re   *regexp.Regexp
}

var piiPatterns = []*piiRule{
	// --- Universal ---
	{name: "email", re: regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)},
	// grouped 4-4-4-4 or bare 13-16 consecutive digits (avoids matching phone/RRN separators)
	{name: "credit_card", re: regexp.MustCompile(`\b(?:\d{4}[ -]){3}\d{4}\b|\b\d{13,16}\b`)},
	{name: "iban", re: regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{11,29}\b`)},
	{name: "swift_bic", re: regexp.MustCompile(`(?i)(?:swift|bic)[\s:：码代号]*[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}(?:[A-Z0-9]{3})?`)},
	{name: "icd_code", re: regexp.MustCompile(`\bICD[-–]?(?:10|11)[-–]\s*[A-Z]\d{2}(?:\.\d{1,4})?\b`)},

	// --- Chinese ---
	{name: "china_id_card", re: regexp.MustCompile(`\b\d{17}[\dXx]\b`)},
	{name: "china_phone", re: regexp.MustCompile(`\b1[3-9]\d{9}\b`)},
	{name: "china_bank_account", re: regexp.MustCompile(`\b\d{16,19}\b`)},
	{name: "china_unified_credit", re: regexp.MustCompile(`\b[0-9A-HJ-NP-RT-Y]{2}\d{6}[0-9A-HJ-NP-RT-Y]{10}\b`)},
	{name: "china_contract_amount", re: regexp.MustCompile(`(?:合同金额|合同价款)[：:\s]*[¥￥]?\d[\d,\.]*(?:万|千万|亿)?元?`)},
	{name: "china_transaction_id", re: regexp.MustCompile(`(?:交易流水号|流水号|交易编号)[：:\s]*[A-Za-z0-9\-]{8,}`)},
	{name: "china_loan_account", re: regexp.MustCompile(`(?:贷款合同[号编]|借款合同编号|贷款账[户号])[：:\s]*[A-Za-z0-9\-]{6,30}`)},
	{name: "china_securities_account", re: regexp.MustCompile(`(?:证券账[户号]|股票账[户号]|基金账[户号])[：:\s]*[A-Za-z0-9]{6,12}`)},
	{name: "china_insurance_policy", re: regexp.MustCompile(`(?:保险单号|保单号|保单编号)[：:\s]*[A-Za-z0-9\-]{8,25}`)},
	{name: "china_patient_id", re: regexp.MustCompile(`(?:患者[Ii][Dd]|患者编号|病历号|住院号)[：:\s]*[A-Za-z0-9\-]{4,20}`)},

	// --- English ---
	// US SSN: 123-45-6789
	{name: "en_us_ssn", re: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	// US/CA phone: +1 (555) 123-4567 or 555-123-4567
	{name: "en_us_phone", re: regexp.MustCompile(`(?:\+1[\s\-]?)?\(?\d{3}\)?[\s\-]\d{3}[\s\-]\d{4}\b`)},
	// UK phone: +44 7700 900123 or 07700 900123
	{name: "en_uk_phone", re: regexp.MustCompile(`(?:\+44\s?|0)7\d{3}[\s\-]?\d{6}\b`)},
	// UK NI number: AB 12 34 56 C
	{name: "en_uk_ni", re: regexp.MustCompile(`\b[A-CEGHJ-PR-TW-Z]{2}\s?\d{2}\s?\d{2}\s?\d{2}\s?[A-D]\b`)},
	// US passport: letter + 8 digits
	{name: "en_us_passport", re: regexp.MustCompile(`\b[A-Z]\d{8}\b`)},
	// Driver's license keyword + alphanumeric
	{name: "en_driver_license", re: regexp.MustCompile(`(?i)(?:driver'?s?\s+licen[sc]e|driving\s+licen[sc]e|dl\s*#?|license\s+number)[\s:]*[A-Z0-9\-]{6,20}`)},
	// Date of birth keyword
	{name: "en_dob", re: regexp.MustCompile(`(?i)(?:date\s+of\s+birth|dob|birth\s*date)[\s:]*\d{1,2}[\s/\-\.]\d{1,2}[\s/\-\.]\d{2,4}`)},
	// SSN keyword variant
	{name: "en_ssn_keyword", re: regexp.MustCompile(`(?i)(?:social\s+security\s+(?:number|no\.?|#)|ssn)[\s:]*\d{3}[\s\-]?\d{2}[\s\-]?\d{4}`)},
	// Medical record / patient number
	{name: "en_medical_record", re: regexp.MustCompile(`(?i)(?:medical\s+record\s+(?:number|no\.?)|mrn|patient\s+(?:id|number|no\.?))[\s:#]*[A-Z0-9\-]{4,20}`)},

	// --- Korean ---
	// RRN (주민등록번호): 123456-1234567
	{name: "kr_rrn", re: regexp.MustCompile(`\b\d{6}-[1-4]\d{6}\b`)},
	// KR phone: 010-1234-5678 (must precede jp_phone — more specific prefix 01x)
	{name: "kr_phone", re: regexp.MustCompile(`\b01[016789][\s\-]\d{3,4}[\s\-]\d{4}\b`)},

	// --- Japanese ---
	// My Number (個人番号): 12-digit
	{name: "jp_my_number", re: regexp.MustCompile(`(?:マイナンバー|個人番号|基礎年金番号)[：:\s]*\d{4}[\s\-]?\d{4}[\s\-]?\d{4}`)},
	// JP phone: 090-1234-5678 or 03-1234-5678
	{name: "jp_phone", re: regexp.MustCompile(`\b0\d{1,4}[\s\-]\d{2,4}[\s\-]\d{4}\b`)},
	// JP postal code: 〒123-4567
	{name: "jp_postal", re: regexp.MustCompile(`〒\d{3}[\s\-]\d{4}`)},
	// JP passport: 2 letters + 7 digits
	{name: "jp_passport", re: regexp.MustCompile(`\b[A-Z]{2}\d{7}\b`)},
	// JP bank account keyword
	{name: "jp_bank_account", re: regexp.MustCompile(`(?:口座番号|預金口座)[：:\s]*\d{7}`)},
	// JP health insurance number
	{name: "jp_health_insurance", re: regexp.MustCompile(`(?:健康保険証|被保険者番号)[：:\s]*[A-Z0-9\-]{8,15}`)},
	// KR passport: M12345678
	{name: "kr_passport", re: regexp.MustCompile(`\b[MR]\d{8}\b`)},
	// KR business registration number: 123-45-67890
	{name: "kr_business_reg", re: regexp.MustCompile(`\b\d{3}-\d{2}-\d{5}\b`)},
	// KR driver's license: 12-34-567890-12
	{name: "kr_driver_license", re: regexp.MustCompile(`\b\d{2}-\d{2}-\d{6}-\d{2}\b`)},
	// RRN keyword variant
	{name: "kr_rrn_keyword", re: regexp.MustCompile(`(?:주민등록번호|주민번호)[：:\s]*\d{6}-[1-4]\d{6}`)},

	// --- European languages (FR / DE / ES) ---
	// FR NIR (numéro de sécurité sociale): 1 23 45 678 901 23
	{name: "fr_nir", re: regexp.MustCompile(`\b[12]\s?\d{2}\s?\d{2}\s?\d{2}\s?\d{3}\s?\d{3}\s?\d{2}\b`)},
	// FR phone: +33 6 12 34 56 78 or 06 12 34 56 78
	{name: "fr_phone", re: regexp.MustCompile(`(?:\+33\s?|0)[67]\s?\d{2}\s?\d{2}\s?\d{2}\s?\d{2}`)},
	// FR NIR keyword
	{name: "fr_nir_keyword", re: regexp.MustCompile(`(?i)(?:numéro de sécurité sociale|n[°º]\s*sécu|NIR)[\s:]*[12]\s?\d{2}`)},
	// DE Steueridentifikationsnummer: 11 digits
	{name: "de_tax_id", re: regexp.MustCompile(`(?i)(?:steuer(?:liche\s+)?identifikationsnummer|steuer-?id|steuernummer)[\s:]*\d[\s]?\d{3}[\s]?\d{3}[\s]?\d{3}[\s]?\d`)},
	// DE phone: +49 170 1234567 or 0170 1234567
	{name: "de_phone", re: regexp.MustCompile(`(?:\+49\s?|0)1[567]\d{1,2}[\s\-]?\d{6,8}\b`)},
	// DE passport: C01X0006 (letter + 8 alphanumeric)
	{name: "de_passport", re: regexp.MustCompile(`\b[A-Z][0-9A-Z]{8}\b`)},
	// ES DNI/NIE: 12345678A or X1234567A
	{name: "es_dni_nie", re: regexp.MustCompile(`\b(?:\d{8}[A-HJ-NP-TV-Z]|[XYZ]\d{7}[A-HJ-NP-TV-Z])\b`)},
	// ES phone: +34 612 345 678 or 612345678
	{name: "es_phone", re: regexp.MustCompile(`(?:\+34\s?)?[67]\d{2}[\s\-]?\d{3}[\s\-]?\d{3}\b`)},
	// ES NIE keyword
	{name: "es_nie_keyword", re: regexp.MustCompile(`(?i)(?:n[úu]mero\s+de\s+identificaci[oó]n\s+extranjero|DNI|NIE)[\s:]*[XYZ0-9]\d{7}[A-Z]`)},

	// --- Arabic (Saudi Arabia, UAE, Egypt, Kuwait, Qatar, Morocco, Tunisia, Algeria) ---
	// SA National ID / Iqama: 10 digits, citizens start with 1, residents (Iqama) with 2
	{name: "ar_sa_national_id", re: regexp.MustCompile(`(?:رقم الهوية|الهوية الوطنية|رقم الإقامة|هوية|إقامة)[:\s]*[12]\d{9}`)},
	{name: "ar_sa_id_bare", re: regexp.MustCompile(`\b[12]\d{9}\b`)},
	// UAE Emirates ID: 784-YYYY-XXXXXXX-C
	{name: "ar_uae_emirates_id", re: regexp.MustCompile(`\b784-\d{4}-\d{7}-\d\b`)},
	// UAE Emirates ID keyword
	{name: "ar_uae_id_keyword", re: regexp.MustCompile(`(?:emirates\s+id|هوية الإمارات|رقم الهوية الإماراتية)[:\s]*784-\d{4}`)},
	// Egypt National ID: 14 digits starting with 2 or 3 (birth century code)
	{name: "ar_eg_national_id", re: regexp.MustCompile(`(?:الرقم القومي|رقم البطاقة القومية|رقم الهوية)[:\s]*[23]\d{13}`)},
	{name: "ar_eg_id_bare", re: regexp.MustCompile(`\b[23]\d{13}\b`)},
	// Kuwait Civil ID: 12 digits
	{name: "ar_kw_civil_id", re: regexp.MustCompile(`(?:الرقم المدني|civil\s+id|رقم الهوية الكويتية)[:\s]*\d{12}`)},
	// Qatar QID: 11 digits starting with 2 or 3
	{name: "ar_qa_qid", re: regexp.MustCompile(`(?:رقم القيد|رقم الهوية القطرية|QID)[:\s]*[23]\d{10}`)},
	// Morocco CIN: 1-2 uppercase letters + 6 digits (keyword required — pattern too generic alone)
	{name: "ar_ma_cin", re: regexp.MustCompile(`(?:رقم بطاقة التعريف الوطنية|CIN|carte\s+nationale)[:\s]*[A-Z]{1,2}\d{6}`)},
	// Tunisia CIN: 8 digits
	{name: "ar_tn_cin", re: regexp.MustCompile(`(?:رقم بطاقة التعريف|CIN tunisien|numéro\s+CIN)[:\s]*\d{8}`)},
	// Algeria NIN (Numéro d'Identification National): 18 digits
	{name: "ar_dz_nin", re: regexp.MustCompile(`(?:رقم التعريف الوطني|NIN|numéro\s+d.identification)[:\s]*\d{18}`)},
	// Arabic passport keyword (pan-Arab)
	{name: "ar_passport", re: regexp.MustCompile(`(?:رقم جواز السفر|جواز السفر|passport\s+no)[:\s]*[A-Z]{1,2}\d{6,8}`)},
	// Arabic bank account keyword (pan-Arab)
	{name: "ar_bank_account", re: regexp.MustCompile(`(?:رقم الحساب البنكي|رقم الحساب|رقم الآيبان)[:\s]*[A-Z0-9\-]{10,34}`)},
	// Saudi Arabia phone: +966 5x xxxx xxxx
	{name: "ar_sa_phone", re: regexp.MustCompile(`(?:\+966\s?|0)5\d[\s\-]?\d{4}[\s\-]?\d{4}\b`)},
	// UAE phone: +971 5x xxx xxxx
	{name: "ar_uae_phone", re: regexp.MustCompile(`(?:\+971\s?|0)5[024568][\s\-]?\d{3}[\s\-]?\d{4}\b`)},
	// Egypt phone: +20 1x xxxx xxxx
	{name: "ar_eg_phone", re: regexp.MustCompile(`(?:\+20\s?|0)1[0125][\s\-]?\d{4}[\s\-]?\d{4}\b`)},
	// Jordan phone: +962 7x xxx xxxx
	{name: "ar_jo_phone", re: regexp.MustCompile(`(?:\+962\s?|0)7[789][\s\-]?\d{4}[\s\-]?\d{3}\b`)},
	// Kuwait phone: +965 xxxx xxxx
	{name: "ar_kw_phone", re: regexp.MustCompile(`(?:\+965\s?)[569]\d{3}[\s\-]?\d{4}\b`)},
	// Qatar phone: +974 xxxx xxxx
	{name: "ar_qa_phone", re: regexp.MustCompile(`(?:\+974\s?)[3567]\d{3}[\s\-]?\d{4}\b`)},
	// Lebanon phone: +961 7x/3x xxx xxx
	{name: "ar_lb_phone", re: regexp.MustCompile(`(?:\+961\s?|0)[37]\d[\s\-]?\d{3}[\s\-]?\d{3}\b`)},

	// --- Italy ---
	// Codice Fiscale: 6 letters + 2 digits + letter + 2 digits + letter + 3 digits + letter
	{name: "it_codice_fiscale", re: regexp.MustCompile(`\b[A-Z]{6}\d{2}[A-Z]\d{2}[A-Z]\d{3}[A-Z]\b`)},
	// IT VAT (Partita IVA): IT + 11 digits
	{name: "it_vat", re: regexp.MustCompile(`(?i)\bIT\d{11}\b`)},
	// IT phone: +39 3xx xxxxxxx
	{name: "it_phone", re: regexp.MustCompile(`(?:\+39\s?|0)3\d{2}[\s\-]?\d{6,7}\b`)},
	// IT codice fiscale keyword
	{name: "it_cf_keyword", re: regexp.MustCompile(`(?i)(?:codice\s+fiscale|c\.f\.)[\s:]*[A-Z]{6}\d{2}[A-Z]\d{2}[A-Z]\d{3}[A-Z]`)},

	// --- Netherlands ---
	// BSN (Burgerservicenummer): 9 digits
	{name: "nl_bsn", re: regexp.MustCompile(`(?i)(?:bsn|burgerservicenummer|sofinummer)[\s:]*\d{9}`)},
	// NL phone: +31 6 12345678 or 06-12345678
	{name: "nl_phone", re: regexp.MustCompile(`(?:\+31\s?|0)6[\s\-]?\d{8}\b`)},
	// NL passport: 2 letters + 7 digits
	{name: "nl_passport", re: regexp.MustCompile(`(?i)(?:paspoort|passport)[\s:#]*[A-Z]{2}\d{7}`)},

	// --- Poland ---
	// PESEL: 11 digits (YYMMDDXXXXXC)
	{name: "pl_pesel", re: regexp.MustCompile(`(?i)(?:pesel)[\s:]*\d{11}`)},
	// NIP (tax): 10 digits formatted XXX-XXX-XX-XX or plain
	{name: "pl_nip", re: regexp.MustCompile(`(?i)(?:nip)[\s:]*\d{3}[\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}`)},
	// PL phone: +48 xxx xxx xxx
	{name: "pl_phone", re: regexp.MustCompile(`(?:\+48\s?)?\b[4-9]\d{2}[\s\-]?\d{3}[\s\-]?\d{3}\b`)},

	// --- Sweden ---
	// Personnummer: YYYYMMDD-XXXX or YYMMDD-XXXX
	{name: "se_personnummer", re: regexp.MustCompile(`\b(?:\d{8}|\d{6})[\-\+]\d{4}\b`)},
	// SE personnummer keyword
	{name: "se_personnummer_keyword", re: regexp.MustCompile(`(?i)(?:personnummer|person-?nr)[\s:]*\d{6,8}[\-\+]\d{4}`)},
	// SE phone: +46 70 123 45 67
	{name: "se_phone", re: regexp.MustCompile(`(?:\+46\s?|0)7[0-9][\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}\b`)},

	// --- Portugal ---
	// NIF (Número de Identificação Fiscal): 9 digits starting 1-3 or 5-9
	{name: "pt_nif", re: regexp.MustCompile(`(?i)(?:nif|n[úu]mero\s+de\s+contribuinte)[\s:]*[1-9]\d{8}`)},
	// PT phone: +351 9xx xxx xxx
	{name: "pt_phone", re: regexp.MustCompile(`(?:\+351\s?)?9[1236]\d[\s\-]?\d{3}[\s\-]?\d{3}\b`)},

	// --- Belgium ---
	// National register: XX.XX.XX-XXX.XX
	{name: "be_national_nr", re: regexp.MustCompile(`\b\d{2}\.\d{2}\.\d{2}[\-]\d{3}\.\d{2}\b`)},
	// BE national register keyword
	{name: "be_national_keyword", re: regexp.MustCompile(`(?i)(?:numéro\s+national|rijksregisternummer|national\s+number)[\s:]*\d{2}[\.\-]?\d{2}[\.\-]?\d{2}`)},
	// BE phone: +32 4xx xx xx xx
	{name: "be_phone", re: regexp.MustCompile(`(?:\+32\s?|0)4\d{2}[\s\-]?\d{2}[\s\-]?\d{2}[\s\-]?\d{2}\b`)},

	// --- Switzerland ---
	// AHV (AVS) number: 756.XXXX.XXXX.XX
	{name: "ch_ahv", re: regexp.MustCompile(`\b756\.\d{4}\.\d{4}\.\d{2}\b`)},
	// CH AHV keyword
	{name: "ch_ahv_keyword", re: regexp.MustCompile(`(?i)(?:ahv[\-\s]?nr|avs|sozialversicherungsnummer)[\s:]*756\.\d{4}`)},
	// CH phone: +41 7x xxx xx xx
	{name: "ch_phone", re: regexp.MustCompile(`(?:\+41\s?|0)7[5-9][\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}\b`)},

	// --- Denmark ---
	// CPR (Det Centrale Personregister): DDMMYY-XXXX
	{name: "dk_cpr", re: regexp.MustCompile(`\b\d{6}[\-]\d{4}\b`)},
	// DK CPR keyword
	{name: "dk_cpr_keyword", re: regexp.MustCompile(`(?i)(?:cpr[\-\s]?(?:nummer|nr\.?)|personnummer)[\s:]*\d{6}[\-]\d{4}`)},
	// DK phone: +45 xx xx xx xx
	{name: "dk_phone", re: regexp.MustCompile(`(?:\+45\s?)?\b[2-9]\d[\s\-]?\d{2}[\s\-]?\d{2}[\s\-]?\d{2}\b`)},

	// --- Finland ---
	// HETU (henkilötunnus): DDMMYY[+-A]XXXN
	{name: "fi_hetu", re: regexp.MustCompile(`\b\d{6}[+\-A]\d{3}[A-Z0-9]\b`)},
	// FI HETU keyword
	{name: "fi_hetu_keyword", re: regexp.MustCompile(`(?i)(?:hetu|henkilötunnus|henkilotunnus|Finnish\s+ID)[\s:]*\d{6}[+\-A]`)},
	// FI phone: +358 4x xxx xxxx
	{name: "fi_phone", re: regexp.MustCompile(`(?:\+358\s?|0)4[0-9][\s\-]?\d{3}[\s\-]?\d{4}\b`)},

	// --- Norway ---
	// Fødselsnummer: 11 digits (DDMMYYXXXNN)
	{name: "no_fodselsnummer", re: regexp.MustCompile(`(?i)(?:f[øo]dselsnummer|personnummer|f-?nr)[\s:]*\d{11}`)},
	// NO phone: +47 4xx xx xxx or +47 9xx xx xxx
	{name: "no_phone", re: regexp.MustCompile(`(?:\+47\s?)?[49]\d{2}[\s\-]?\d{2}[\s\-]?\d{3}\b`)},
}

// ViolationRecorder is the interface PIIMiddleware uses to record violations.
// This avoids an import cycle with the storage package.
type ViolationRecorder interface {
	RecordViolation(tenantID, userID, piiType, action, requestPath string)
}

// NewPIIMiddleware creates a PII detection middleware.
// recorder may be nil (no persistence). tenantID is used as a fallback when
// no authenticated user is present in the context; pass "" to always pull from
// the "user" local set by NewAuthMiddleware.
// siemChan is an optional channel for non-blocking SIEM event delivery; pass
// nil to disable SIEM integration.
func NewPIIMiddleware(recorder ViolationRecorder, tenantID string, siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		for _, rule := range piiPatterns {
			if rule.re.MatchString(body) {
				tid := tenantID
				uid := ""
				if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
					tid = user.TenantID
					uid = user.UserID
				}
				if recorder != nil {
					recorder.RecordViolation(tid, uid, rule.name, "blocked", c.Path())
				}
				if siemChan != nil {
					select {
					case siemChan <- SIEMEvent{
						TenantID:  tid,
						EventType: "pii_violation",
						Payload: map[string]any{
							"source":      "totra",
							"tenant_id":   tid,
							"event_type":  "pii_violation",
							"occurred_at": time.Now().UTC().Format(time.RFC3339),
							"detail": map[string]any{
								"user_id":  uid,
								"pii_type": rule.name,
								"action":   "blocked",
								"path":     c.Path(),
							},
						},
					}:
					default: // drop if full
					}
				}
				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error": fiber.Map{
						"message": "request blocked: potential PII detected (" + rule.name + ")",
						"type":    "pii_blocked",
					},
				})
			}
		}
		return c.Next()
	}
}

// ScanForPII scans text against all PII patterns. Returns the matched PII type
// name and true on the first match; returns ("", false) if no PII is found.
func ScanForPII(text string) (piiType string, found bool) {
	for _, rule := range piiPatterns {
		if rule.re.MatchString(text) {
			return rule.name, true
		}
	}
	return "", false
}
