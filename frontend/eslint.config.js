import eslint from '@eslint/js'
import tseslint from 'typescript-eslint'
import reactPlugin from 'eslint-plugin-react'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import playwright from 'eslint-plugin-playwright'

export default tseslint.config(
  { ignores: ['dist', 'node_modules', 'e2e-report', 'test-results', 'e2e/.stack', 'e2e/.auth'] },
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  {
    plugins: {
      react: reactPlugin,
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    settings: {
      react: { version: 'detect' },
    },
    rules: {
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
    },
  },
  {
    // The end-to-end tests. The rules keep them honest: a test must wait for a
    // state rather than for time, must not branch, and must assert something.
    files: ['e2e/**/*.ts'],
    ...playwright.configs['flat/recommended'],
    rules: {
      ...playwright.configs['flat/recommended'].rules,
      'playwright/no-wait-for-timeout': 'error',
      'playwright/no-conditional-in-test': 'error',
      'playwright/no-conditional-expect': 'error',
      'playwright/no-skipped-test': 'error',
      'playwright/no-focused-test': 'error',
      'playwright/expect-expect': ['error', { assertFunctionNames: ['expect', 'knownBug'] }],
      '@typescript-eslint/no-empty-object-type': 'off',
      // Playwright's fixture callback is called "use"; it is not a React hook.
      'react-hooks/rules-of-hooks': 'off',
    },
  },
  {
    // The stack launcher is plain Node, not a browser module.
    files: ['e2e/**/*.mjs'],
    languageOptions: { globals: { process: 'readonly' } },
  },
)
