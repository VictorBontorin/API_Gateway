.PHONY: tidy test run up
tidy:
	go mod tidy
test:
	go test ./... -race
run:
	set -a; . ./.env.example; set +a; go run ./cmd/gateway
up:
	docker compose up --build
