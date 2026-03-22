# Contributing to Hermes

First off, thank you for considering contributing to Hermes! It's people like you that make open-source software great.

## The Hermes Philosophy
Hermes is designed to be a clean, platform-agnostic **Interface Layer** for chat applications. When contributing, please keep these core tenets in mind:
1. **Universal First, Specific Second:** The core `Message` and `SentMessage` structs should handle 95% of use cases. Use the `Metadata` map as an escape hatch for platform-specific quirks (like Slack threads or Discord snowflakes).
2. **No "Parameter Soup":** Methods should take rich structs or references (like `*SentMessage`) rather than long lists of raw strings.
3. **Graceful Degradation:** If a platform doesn't support a feature (e.g., Telegram doesn't support a specific audio format), the provider should handle it gracefully or return a clear, wrapped error, rather than panicking.

## Development Setup
1. Ensure you have Go 1.25 or later installed.
2. Fork the repository and clone it locally.
3. Run `go mod tidy` to ensure dependencies are synced.

## Pull Request Process
1. **Branch Naming:** Create a branch for your feature (`feat/discord-provider`) or bugfix (`fix/telegram-album-crash`).
2. **Testing:** Write unit tests for your changes. Run `go test -v ./...` before committing.
3. **Formatting:** Ensure your code passes standard Go formatting by running `go fmt ./...`.
4. **Documentation:** If you change the public API, update the doc comments.

## Adding a New Provider
If you are building a new platform provider (e.g., Discord, Slack):
* Ensure it implements the full `hermes.Provider` interface.
* Create a separate sub-package (e.g., `/discord`).
* Keep mapping logic isolated from network/polling logic.