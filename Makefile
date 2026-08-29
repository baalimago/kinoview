.PHONY: qa
qa:
	go test ./... -race -count=3 -cover -timeout=30s
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	go run mvdan.cc/gofumpt@latest -w -l .
	go vet ./...
	go fix ./...
	go run github.com/mibk/dupl@latest -t 80 .

# The slivingdoc S3 backend (docker-compose.yml) as a standalone Docker stack.
.PHONY: s3-up s3-down s3-logs
s3-up:
	docker compose up -d --build
s3-down:
	docker compose down
s3-logs:
	docker compose logs -f
