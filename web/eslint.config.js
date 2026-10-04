import js from '@eslint/js'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import { defineConfig } from 'eslint/config'
import i18next from 'eslint-plugin-i18next'
import tseslint from 'typescript-eslint'

const USER_FACING_ATTRIBUTES =
  '^(title|alt|placeholder|label|message|description|hint|detail|aria-label|aria-description|aria-placeholder|aria-roledescription|aria-valuetext)$'

/**
 * Literals that are not prose: no lowercase letters (symbols, numbers, and
 * acronyms such as MCP), ids and keys, URLs and paths, class-name lists, and
 * names that are the same in every language.
 */
const NOT_PROSE = [
  /^[^\p{Ll}]*$/u,
  /^[a-z][a-z0-9_.:-]*$/,
  /^\S*[/@:]\S*$/,
  /^(\S*[-\d]\S*\s*)+$/,
  /^(Toskar|Yggdrasil|Ratatoskr|Huginn|Muninn|Mimir|Heimdall|Bifrost|Gjallarhorn|Norn|Ymir|Brokkr|Odin|Gungnir|Valgrind|Forseti|PostgreSQL|MySQL|SQLite)$/,
]

export default defineConfig(
  { ignores: ['dist', 'node_modules', 'wailsjs'] },
  js.configs.recommended,
  tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      // The plugin's recommended set also enables React Compiler rules that
      // rewrite existing effects. This pass enforces the hook rules.
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
    },
  },
  {
    // Text people read comes from the translation catalog (i18n/locales),
    // never a literal in JSX. See docs/features/multilingual-localization-and-language-routing.md §5.
    files: ['src/**/*.tsx'],
    ignores: ['src/**/*.test.tsx', 'src/test/**'],
    plugins: { i18next },
    rules: {
      'i18next/no-literal-string': [
        'error',
        {
          mode: 'jsx-only',
          // Attributes people read or hear; the rest (class names, routes, ids) are code.
          'jsx-attributes': { include: [USER_FACING_ATTRIBUTES] },
          words: { exclude: NOT_PROSE },
        },
      ],
    },
  },
)
