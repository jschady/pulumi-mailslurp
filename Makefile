PROJECT_NAME := Pulumi MailSlurp Resource Provider

PACK             := mailslurp
PACKDIR          := sdk
PROJECT          := github.com/jschady/pulumi-mailslurp
NODE_MODULE_NAME := pulumi-mailslurp
NUGET_PKG_NAME   := Jschady.Mailslurp
PYTHON_PKG_NAME  := pulumi_mailslurp

PROVIDER         := pulumi-resource-${PACK}
PROVIDER_PATH    := provider
VERSION_PATH     := ${PROVIDER_PATH}/pkg/version.Version
SCHEMA_PATH      := ${PROVIDER_PATH}/cmd/${PROVIDER}/schema.json

WORKING_DIR      := $(shell pwd)
TESTPARALLELISM  := 4

PULUMI           := pulumi
GOLANGCI_LINT    := golangci-lint
GOTEST           := go test

# Override during CI with `make [TARGET] PROVIDER_VERSION=x.y.z` or the environment.
PROVIDER_VERSION ?= 0.1.0-alpha.0+dev
VERSION_GENERIC = $(shell pulumictl convert-version --language generic --version "$(PROVIDER_VERSION)")

# Files that lint_prose reads. Override to check one file: make lint_prose PROSE_FILES=x.md
PROSE_FILES ?=

# The converter that reads the YAML examples. A floating version would move the generated docs.
YAML_CONVERTER_VERSION := 1.38.3

ifeq ($(shell printf '%.1s' "$(PROVIDER_VERSION)"),v)
$(error PROVIDER_VERSION must not start with a v: $(PROVIDER_VERSION))
endif

export PULUMI_IGNORE_AMBIENT_PLUGINS = true
export PULUMI_DISABLE_AUTOMATIC_PLUGIN_ACQUISITION = true
export PULUMI_HOME = $(WORKING_DIR)/.pulumi
export PULUMI_LOCAL_NUGET = $(WORKING_DIR)/nuget
export PULUMI_BACKEND_URL = file://$(WORKING_DIR)/.pulumi-state
export PULUMI_CONFIG_PASSPHRASE = ci-local-passphrase

.PHONY: ensure
ensure: tidy lint test_provider

# Every module the repo may carry. The wildcard drops the SDK until it is generated.
GO_MODULES := $(dir $(wildcard go.mod sdk/go/$(PACK)/go.mod))

.PHONY: tidy
tidy:
	for module in $(GO_MODULES); do (cd $$module && go mod tidy); done

# Every example test file sits behind a build tag, so the linter reads none of them without this.
LINT_BUILD_TAGS := all

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run -c .golangci.yml --build-tags $(LINT_BUILD_TAGS)

.PHONY: lint_fix
lint_fix:
	$(GOLANGCI_LINT) run --fix -c .golangci.yml --build-tags $(LINT_BUILD_TAGS)

.PHONY: lint_prose
lint_prose:
	./scripts/lint-prose.sh $(PROSE_FILES)

.PHONY: provider
provider:
	@test -d $(PROVIDER_PATH)/cmd/$(PROVIDER) || { echo "make provider needs $(PROVIDER_PATH)/cmd/$(PROVIDER). Restore it from the repository."; exit 1; }
	go build -o bin/$(PROVIDER) -ldflags "-X $(PROJECT)/$(VERSION_PATH)=$(VERSION_GENERIC)" $(PROJECT)/$(PROVIDER_PATH)/cmd/$(PROVIDER)

.PHONY: generate_schema
generate_schema: provider
	$(PULUMI) package get-schema ./bin/$(PROVIDER) | jq 'del(.version)' > $(SCHEMA_PATH)

.PHONY: codegen
codegen:
	@echo "codegen does nothing here. This provider is native, so it has no bridge step."

.PHONY: sdk
sdk: generate_nodejs generate_python generate_dotnet generate_go \
	build_nodejs build_python build_dotnet build_go \
	install_nodejs_sdk install_python_sdk install_dotnet_sdk install_go_sdk

# Generation reads the committed schema, not the built binary: the binary stamps its build version
# into every manifest. A token in the environment would reach the plugin home, and none is needed.
gen_sdk = PULUMI_ACCESS_TOKEN= $(PULUMI) package gen-sdk $(SCHEMA_PATH) --language $(1) -o $(WORKING_DIR)/sdk

# Read from the provider so the SDK never claims a different Go or Pulumi version.
SDK_GO_VERSION = $(shell go list -m -f '{{.GoVersion}}')
SDK_PULUMI_VERSION = $(shell go list -m -f '{{.Version}}' github.com/pulumi/pulumi/sdk/v3)

# gen-sdk replaces its language directory whole, so a committed file inside one survives only when
# a recipe writes it back. Each build stages its artifact under the SDK's own gitignored bin/.
.PHONY: generate_nodejs
generate_nodejs:
	$(call gen_sdk,nodejs)

# The generator still writes the license as a TOML table, which setuptools deprecated in favor
# of an SPDX string (removal 2027-02, string form needs setuptools>=77). Collapse it after
# generation. https://packaging.python.org/en/latest/guides/writing-pyproject-toml/#license
.PHONY: generate_python
generate_python:
	$(call gen_sdk,python)
	perl -0pi -e 's/\[project\.license\]\n\s*text = ("[^"]+")/license = $$1/; s/"setuptools>=61\.0"/"setuptools>=77.0.0"/' sdk/python/pyproject.toml

# The .NET generator downloads the schema's logo URL and writes the response body to logo.png,
# whatever it is. https://github.com/pulumi/pulumi/issues/13589
.PHONY: generate_dotnet
generate_dotnet:
	$(call gen_sdk,dotnet)
	cp docs/logo.png sdk/dotnet/logo.png
	cp README.md sdk/dotnet/README.md

.PHONY: generate_go
generate_go:
	$(call gen_sdk,go)
	cd sdk/go/$(PACK) && \
		go mod init $(PROJECT)/sdk/go/$(PACK) && \
		go mod edit -go=$(SDK_GO_VERSION) -require=github.com/pulumi/pulumi/sdk/v3@$(SDK_PULUMI_VERSION) && \
		go mod tidy

# The generated package.json carries a $${VERSION} placeholder that npm cannot parse, so the
# staged copy is the one that gets the build version.
.PHONY: build_nodejs
build_nodejs: generate_nodejs
	cd sdk/nodejs && \
		yarn install --no-progress && \
		yarn run tsc && \
		cp ../../README.md ../../LICENSE bin/ && \
		sed -e 's/$${VERSION}/$(VERSION_GENERIC)/g' package.json > bin/package.json

# pyproject.toml names a README the generator does not write and carries a placeholder version,
# so the wheel is built from a staged copy that has both.
.PHONY: build_python
build_python: PYPI_VERSION := $(shell pulumictl convert-version --language python --version "$(PROVIDER_VERSION)")
build_python: generate_python
	cd sdk/python && \
		rm -rf bin venv && \
		python3 -m venv venv && \
		./venv/bin/python -m pip install --quiet build && \
		mkdir -p bin && \
		cp -R $(PYTHON_PKG_NAME) bin/ && \
		cp ../../README.md bin/ && \
		sed -E -e 's/^([[:space:]]*)version = "0\.0\.0"$$/\1version = "$(PYPI_VERSION)"/' pyproject.toml > bin/pyproject.toml && \
		cd bin && ../venv/bin/python -m build --outdir dist .

# The generated project declares no version, so a plain build packs the .NET default of 1.0.0.
.PHONY: build_dotnet
build_dotnet: DOTNET_VERSION := $(shell pulumictl convert-version --language dotnet --version "$(PROVIDER_VERSION)")
build_dotnet: generate_dotnet
	cd sdk/dotnet && echo "$(DOTNET_VERSION)" > version.txt && dotnet build -p:Version=$(DOTNET_VERSION)

.PHONY: build_go
build_go: generate_go
	cd sdk/go/$(PACK) && go build ./...

.PHONY: install_nodejs_sdk
install_nodejs_sdk: build_nodejs
	-yarn unlink --cwd sdk/nodejs/bin
	yarn link --cwd sdk/nodejs/bin

.PHONY: install_python_sdk
install_python_sdk: build_python
	@ls sdk/python/bin/dist/*.whl

# A package the feed still holds from an earlier build version outranks the one just built.
.PHONY: install_dotnet_sdk
install_dotnet_sdk: build_dotnet
	mkdir -p $(PULUMI_LOCAL_NUGET)
	rm -f $(PULUMI_LOCAL_NUGET)/*.nupkg
	find sdk/dotnet/bin -name '*.nupkg' -exec cp -p {} $(PULUMI_LOCAL_NUGET) \;

.PHONY: install_go_sdk
install_go_sdk: build_go
	@echo "A Go program reaches sdk/go/$(PACK) by its module path or a replace directive."

.PHONY: test_provider
test_provider:
	$(GOTEST) -race -short ./provider/...

# This check regenerates the four SDKs, which needs the Pulumi CLI, so -short keeps it out of
# test_provider.
.PHONY: test_generated
test_generated:
	$(GOTEST) -count=1 -run TestGeneratingTheSDKsLeavesTheTreeClean ./provider/...

# The generation check rewrites four SDK directories, so a live run that included it destroyed the
# staged builds every time. Its own target above is the caller that wants it.
.PHONY: test_integration
test_integration:
	$(GOTEST) -race -count=1 -tags=integration -timeout 30m \
		-skip TestGeneratingTheSDKsLeavesTheTreeClean ./provider/...

# The YAML program legs and pulumi convert read their plugins from the Pulumi home: ambient
# discovery and automatic acquisition are off, so only an installed plugin resolves. The provider
# binary comes from bin/, built by the provider target or unpacked from the build artifact in CI.
.PHONY: install_plugins
install_plugins:
	$(PULUMI) plugin install resource $(PACK) $(VERSION_GENERIC) --file bin/$(PROVIDER) --reinstall
	$(PULUMI) plugin install converter yaml $(YAML_CONVERTER_VERSION)

# A cached pass of a live run reads exactly like a fresh one, so the count switch forbids the cache.
.PHONY: test_examples
test_examples: install_plugins
	$(GOTEST) -count=1 -v -tags=all -timeout 2h -parallel $(TESTPARALLELISM) ./examples/...

# go vet builds the test package without running it, so no TestMain reaches the API here.
.PHONY: compile_examples
compile_examples: compile_nodejs_example compile_python_example compile_dotnet_example compile_go_example
	go vet -tags=all ./examples/...

.PHONY: compile_nodejs_example
compile_nodejs_example:
	cd examples/nodejs && yarn install --no-progress && \
		{ yarn link $(NODE_MODULE_NAME) || { echo "Run make install_nodejs_sdk first."; exit 1; }; } && \
		yarn run tsc --noEmit

.PHONY: compile_python_example
compile_python_example:
	cd examples/python && rm -rf venv && python3 -m venv venv && \
		./venv/bin/python -m pip install --quiet -r requirements.txt ../../sdk/python/bin && \
		./venv/bin/python -c "import $(PYTHON_PKG_NAME)" && \
		./venv/bin/python -m compileall -q __main__.py

.PHONY: compile_dotnet_example
compile_dotnet_example:
	cd examples/dotnet && dotnet build

.PHONY: compile_go_example
compile_go_example:
	cd examples/go && go build -o /dev/null ./...

.PHONY: examples
examples: provider
	$(MAKE) install_plugins
	$(MAKE) examples/nodejs examples/python examples/dotnet examples/go

.PHONY: examples/nodejs
examples/nodejs:
	./examples/convert.sh nodejs

.PHONY: examples/python
examples/python:
	./examples/convert.sh python

.PHONY: examples/dotnet
examples/dotnet:
	./examples/convert.sh dotnet

.PHONY: examples/go
examples/go:
	./examples/convert.sh go

.PHONY: docs
docs: provider
	$(MAKE) install_plugins
	go generate docs/generate.go

.PHONY: release_snapshot
release_snapshot:
	goreleaser release --snapshot --clean

.PHONY: clean
clean:
	-yarn unlink --cwd sdk/nodejs/bin
	rm -rf sdk/dotnet sdk/nodejs sdk/python sdk/go
	rm -rf bin dist nuget coverage.txt .pulumi .pulumi-state
