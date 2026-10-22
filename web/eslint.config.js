import js from '@eslint/js'
import importX from 'eslint-plugin-import-x'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import globals from 'globals'
import tseslint from 'typescript-eslint'

export default tseslint.config(
    { ignores: ['dist'] },
    {
        extends: [js.configs.recommended, ...tseslint.configs.recommended],
        files: ['**/*.{ts,tsx}'],
        languageOptions: {
            ecmaVersion: 2020,
            globals: globals.browser,
        },
        plugins: {
            'import-x': importX,
            'react-hooks': reactHooks,
            'react-refresh': reactRefresh,
        },
        rules: {
            ...reactHooks.configs.recommended.rules,
            // React Compiler rules from react-hooks 7 that existing effects and refs violate; enable each once its sites are fixed.
            'react-hooks/immutability': 'off',
            'react-hooks/incompatible-library': 'off',
            'react-hooks/preserve-manual-memoization': 'off',
            'react-hooks/refs': 'off',
            'react-hooks/set-state-in-effect': 'off',
            'react-refresh/only-export-components': [
                'warn',
                { allowConstantExport: true },
            ],
            'import-x/order': [
                'warn',
                {
                    "groups":
                        [
                            "builtin",
                            "external",
                            "internal",
                            ["sibling", "parent"],
                            "index",
                            "object",
                            "type"
                        ],
                    "pathGroups": [
                        {
                            "pattern": "@/api/**",
                            "group": "internal"
                        },
                        {
                            "pattern": "@/components/**",
                            "group": "internal",
                            "position": "after"
                        },
                        {
                            "pattern": "@/components/ui/**",
                            "group": "internal",
                            "position": "after"
                        },
                    ],
                    "pathGroupsExcludedImportTypes":
                        ["internal"],
                    "alphabetize": {
                        "order": "asc",
                        "caseInsensitive": true
                    },
                    "newlines-between": "always",
                }
            ]
        },
    },
)
