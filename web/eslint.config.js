import js from '@eslint/js'
import globals from 'globals'
import sortImport from 'eslint-plugin-import'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
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
            'import': sortImport,
            'react-hooks': reactHooks,
            'react-refresh': reactRefresh,
        },
        rules: {
            ...reactHooks.configs.recommended.rules,
            'react-refresh/only-export-components': [
                'warn',
                { allowConstantExport: true },
            ],
            'import/order': [
                1,
                {
                    "groups":
                        [
                            "external",
                            "builtin",
                            "internal",
                            "sibling",
                            "parent",
                            "index"
                        ],
                    "pathGroups": [
                        {
                            "pattern": "api",
                            "group": "internal"
                        },
                        {
                            "pattern": "components",
                            "group": "internal"
                        },
                    ],
                    "pathGroupsExcludedImportTypes":
                        ["internal"],
                    "alphabetize": {
                        "order": "asc",
                        "caseInsensitive": true
                    }

                }
            ]
        },
    },
)
