CANDIDATE_FLAGS :=
LINT_FLAGS :=
ifeq ($(PLUTO_CORE_CANDIDATE_DATA),1)
CANDIDATE_FLAGS := -tags=plutocandidate
LINT_FLAGS := --build-tags=plutocandidate
endif

.PHONY: fmt vet check database

fmt:
	../../../scripts/golangci.sh fmt ./...
	../../../scripts/golangci.sh run $(LINT_FLAGS) --enable-only wsl_v5 --fix ./...
	../../../scripts/golangci.sh fmt ./...
	cd ../../.. && go run ./scripts/commentspacing -write \
	  examples/shipments/postgres

vet:
	cd ../../.. && go run ./scripts/commentspacing \
	  -formatter "$$PWD/scripts/golangci.sh" examples/shipments/postgres
	../../../scripts/lint.sh $(LINT_FLAGS) ./...
	GOWORK=off go test $(CANDIDATE_FLAGS) ./...
	GOWORK=off go vet $(CANDIDATE_FLAGS) ./...
	GOWORK=off go build $(CANDIDATE_FLAGS) ./...

check: vet

database:
	./scripts/database-check.sh
