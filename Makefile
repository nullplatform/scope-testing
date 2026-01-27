.PHONY: test test-all test-unit test-tofu test-integration help

# Default test target - shows available options
test:
	@echo "Usage: make test-<level>"
	@echo ""
	@echo "Available test levels:"
	@echo "  make test-all          Run all tests"
	@echo "  make test-unit         Run BATS unit tests"
	@echo "  make test-tofu         Run OpenTofu tests"
	@echo "  make test-integration  Run integration tests"
	@echo ""
	@echo "You can also run tests for a specific module:"
	@echo "  make test-unit MODULE=frontend"

# Run all tests
test-all: test-unit test-tofu test-integration

# Run BATS unit tests
test-unit:
ifdef MODULE
	@./testing/run_bats_tests.sh $(MODULE)
else
	@./testing/run_bats_tests.sh
endif

# Run OpenTofu tests
test-tofu:
ifdef MODULE
	@./testing/run_tofu_tests.sh $(MODULE)
else
	@./testing/run_tofu_tests.sh
endif

# Run integration tests
test-integration:
ifdef MODULE
	@./testing/run_integration_tests.sh $(MODULE) $(if $(VERBOSE),-v)
else
	@./testing/run_integration_tests.sh $(if $(VERBOSE),-v)
endif

# Help
help:
	@echo "Test targets:"
	@echo "  test              Show available test options"
	@echo "  test-all          Run all tests"
	@echo "  test-unit         Run BATS unit tests"
	@echo "  test-tofu         Run OpenTofu tests"
	@echo "  test-integration  Run integration tests"
	@echo ""
	@echo "Options:"
	@echo "  MODULE=<name>     Run tests for specific module (e.g., MODULE=frontend)"
	@echo "  VERBOSE=1         Show output of passing tests (integration tests only)"