## 📝 Description
Please include a summary of the change and which issue is fixed. If this is a new Provider (e.g., Discord, Slack), explain any platform-specific quirks you encountered.

Fixes # (issue number)

## 🛠 Type of Change
- [ ] 🐛 Bug fix (non-breaking change which fixes an issue)
- [ ] ✨ New feature (non-breaking change which adds functionality)
- [ ] 💥 Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] 📖 Documentation update
- [ ] ⚡ Performance improvement

## 🏗️ Interface Layer Check
- [ ] Does this follow the "Universal First" philosophy?
- [ ] Are new methods using references (e.g., `*SentMessage`) instead of raw strings?
- [ ] Is platform-specific data tucked away in `Metadata`?

## ✅ Checklist
- [ ] My code follows the Go style guidelines of this project.
- [ ] I have performed a self-review of my own code.
- [ ] I have commented my code, particularly in hard-to-understand areas (Doc comments).
- [ ] I have added tests that prove my fix is effective or that my feature works.
- [ ] New and existing unit tests pass locally with `go test ./...`.
- [ ] I have run `go fmt ./...` on my changes.

## 📸 Screenshots (if applicable)
If this changes how messages are displayed or handled visually on a platform, please add screenshots here.