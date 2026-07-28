BIN     := bin
DB      ?= data/holidays.db
ADDR    ?= :8080
YEAR    ?= $(shell date +%Y)

.PHONY: all build test fmt vet clean run scrape status next-year docker

all: build

build:
	@mkdir -p $(BIN)
	go build -trimpath -ldflags="-s -w" -o $(BIN)/khapi ./cmd/api
	go build -trimpath -ldflags="-s -w" -o $(BIN)/khapi-scrape ./cmd/scrape
	@echo "built $(BIN)/khapi and $(BIN)/khapi-scrape"

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test

## run: serve the API
run: build
	$(BIN)/khapi -addr $(ADDR) -db $(DB)

## scrape: fetch the current and next year
scrape: build
	$(BIN)/khapi-scrape scrape -db $(DB)

## status: show coverage and the source audit trail
status: build
	$(BIN)/khapi-scrape status -db $(DB)

## next-year: check whether next year's sub-decree has been published
next-year: build
	$(BIN)/khapi-scrape scrape -year $$(( $(YEAR) + 1 )) -db $(DB)

## seed: backfill a range of years
seed: build
	$(BIN)/khapi-scrape scrape -years 2024-2027 -db $(DB)

docker:
	docker build -t khmer-holiday-api .

clean:
	rm -rf $(BIN)
