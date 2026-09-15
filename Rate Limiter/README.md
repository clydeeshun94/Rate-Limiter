# Rate Limiter

A configurable rate limiting engine implemented in **Go**, with interchangeable algorithms and storage backends.

## Algorithms Implemented

- Fixed Window
- Sliding Window Counter
- Sliding Window Log
- Token Bucket
- Leaky Bucket

Each algorithm is implemented in its own directory with detailed explanations, tests, and performance analysis.

## Stack

- **Language:** Go
- **Storage:** In-memory (first), Redis (later)
- **Testing:** Go's built-in test framework (`go test`)
- **No external dependencies** for core algorithms