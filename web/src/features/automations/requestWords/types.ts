// The words an automation request is read with, one file per language in
// the shared catalog, i18n/requests/<language>.json, which the daemon reads
// too (#204).
//
// Phrases are lowercase and matched against the request after it is
// lowercased and NFKC-normalized (full-width digits become 0-9, ’ becomes ').
// A phrase is plain text with a few marks:
//
//   (a|b)   either word          (only )?  an optional part
//   …       up to 40 characters   #         an amount, such as $500 or 5万円
//
// In a language written with spaces between words, a space in a phrase
// matches any run of spaces, and a phrase matches only whole words. In a
// language written without them (Japanese, Chinese, Korean with its attached
// particles), spaces are optional and phrases match anywhere.
//
// To add a language, copy i18n/requests/en.json, translate the words, and
// add its cases to the tests: web/src/features/automations/parseRequest.test.ts
// and internal/automations/request_test.go read the language's example
// request (form.describePlaceholder in automations.json).

export type Daypart = 'morning' | 'afternoon' | 'evening' | 'night'

export interface RequestWords {
  /** Whether words are separated by spaces: false for Japanese, Chinese, and Korean. */
  spaced: boolean
  /** Number words for amounts of time: "two" → 2, "zwei" → 2. */
  numbers: Record<string, number>
  /**
   * The character for ten in numerals such as 二十 (20), for languages that
   * write numbers in characters. Numerals are read when a counter follows them.
   */
  tens?: string
  /** Counters that follow a numeral written in characters: 点, 時, 分. */
  counters?: string[]

  interval: {
    /** Before the amount: "every", "alle", "cada", "每". */
    before: string[]
    /** After the unit: "ごと", "마다". */
    after: string[]
    units: { second: string[]; minute: string[]; hour: string[]; day: string[] }
    /** Whole phrases for every hour, such as "hourly" or "毎時". */
    hourly: string[]
    /** Whole phrases for every half hour. */
    halfHour: string[]
  }
  /** Phrases that make a request run every day, by the time of day they name. */
  daily: Record<Daypart | 'day', string[]>
  /** Words for a time of day: "evening", "abends", "下午". They also turn 8 into 20:00. */
  dayparts: Record<Daypart, string[]>
  weekly: {
    /** Before a weekday: "every", "jeden", "cada", "每". */
    before: string[]
    /** After a weekday: "마다", "ごと". */
    after: string[]
    /** Each weekday's names, from Sunday (0) to Saturday (6). */
    days: string[][]
    /** Weekday words that mean "every" on their own, such as "freitags". Indexed like days. */
    alone?: string[][]
  }
  once: {
    once: string[]
    today: string[]
    tomorrow: string[]
    /** Phrases that contain the word for tomorrow but do not mean it, such as "por la mañana". */
    notTomorrow?: string[]
  }
  clock: {
    /** Before an hour: "at", "um", "a las". */
    at: string[]
    /** After an hour: "uhr", "h", "時", "시", "点". */
    hour: string[]
    /** After minutes: "分", "분". */
    minute: string[]
    /** Half past: "y media", "半", "반". */
    half: string[]
    /** Twelve-hour markers: "a.m."/"p.m.", "午前"/"午後", "오전"/"오후". */
    am: string[]
    pm: string[]
    /** Whether the marker comes before the time (午後6時) rather than after it (6 PM). */
    meridiemFirst: boolean
  }
  notify: {
    /** Store the result without a notification. */
    none: string[]
    /** Notify when the result changes. */
    change: string[]
    /** Notify when the result is worth it. */
    significant: string[]
    /** Notify when an item is in stock. */
    available: string[]
    /** Before an amount: "below", "unter", "低于". */
    below: string[]
    above: string[]
    /** After an amount: "以下", "이하". */
    belowAfter: string[]
    aboveAfter: string[]
  }
  /** Currency symbols and words, lowercase, with their ISO 4217 codes: "€" → EUR, "円" → JPY. */
  currencies: Record<string, string>
  /** Words after a number that multiply it: 万 → 10000. */
  multipliers: Record<string, number>
  /** The currency of an amount written without one. */
  currency: string
  /** Words that name the automation: a stock check, a release check, research. */
  names: { stock: string[]; release: string[]; research: string[] }
  task: {
    /**
     * Endings removed from the task because the notification already says
     * them, such as " and tell me if the price is below #". Matched at the end.
     */
    dropAtEnd: string[]
    /** Words for price. A price check whose task lacks them gets reportPrice. */
    price: string[]
    /** A sentence added to a price check's task: "Report the current price". */
    reportPrice: string
    /** What ends a sentence and starts the next: ". " or "。". */
    fullStop: string
  }
}
