# Changelog

All notable changes to this project are documented in this file.

This project follows the "Keep a Changelog" format and Semantic Versioning.

Generated: 2026-04-20

## [Unreleased]

Summary of changes since the last release.

### Added

- GitHub integration — Added GitHub OAuth flow and commit-fetching integration. (Commit: 097573c, 2026-04-20; Author: Gabriel Farias)
- Gemini AI integration — Added `GenerationConfig` (temperature, topP, topK, maxOutputTokens) and `WithGenerationConfig()` helper; client now sends `generationConfig` in requests and uses sensible defaults (temperature=0.4); HTTP client timeout increased to 30s. (File: internal/integration/gemini.go)
- Report response enhancements — Include `generated_at` timestamp and `report_type` fields in report responses for improved traceability. (Commit: 94f8eae, 2026-04-20; Author: Gabriel Farias)
- JWT refresh rotation — Implement refresh-token rotation in the authentication middleware to improve security. (Commit: 860f80d, 2026-04-20; Author: Gabriel Farias)

### Changed

- Achievements removal — Removed the achievements domain and repository; dropped the `achievements` table and related index from the initial database schema. (Commits: 70dd7fb, 7204997; 2026-04-20; Author: Gabriel Farias)

### Contributors

- Gabriel Farias — primary contributor for the changes listed above.

### Notes

- This changelog was generated from commit messages. For formal releases, add a version header (for example `## [v0.1.0] - 2026-04-20`) and move items from `Unreleased` under that version.
- If you prefer the changelog in Portuguese or want different phrasing, I can translate or adjust it.
