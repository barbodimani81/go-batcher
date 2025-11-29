# go-batcher

A tiny in-memory batcher for Go.  
It accumulates items and flushes them in batches, triggered either by **batch size** or **timeout**.  
Useful when you want to reduce database calls, API calls, or any I/O by batching work.

---

## Features

- Flush by **size**, **timeout**, or **both**
- Thread-safe
- Pluggable handler function
- Lightweight: no external dependencies

---

## Install

```bash
go get github.com/barbodimani81/go-batcher
