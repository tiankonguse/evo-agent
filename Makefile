BINARY_NAME = evo_agent
BUILD_DIR   = build
SRC_DIR     = src

.PHONY: all build clean run

all: build

build:
	@mkdir -p $(BUILD_DIR)
	cd $(SRC_DIR) && go build -o ../$(BUILD_DIR)/$(BINARY_NAME) .

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

clean:
	rm -f $(BUILD_DIR)/$(BINARY_NAME)

deps:
	cd $(SRC_DIR) && go mod tidy
