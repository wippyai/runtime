module github.com/ponyruntime/pony

go 1.24.0

require (
	connectrpc.com/connect v1.18.1
	github.com/CloudyKit/jet/v6 v6.3.1
	github.com/Masterminds/semver/v3 v3.2.1
	github.com/Masterminds/squirrel v1.5.4
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/aws/aws-sdk-go-v2 v1.36.3
	github.com/aws/aws-sdk-go-v2/config v1.29.14
	github.com/aws/aws-sdk-go-v2/service/s3 v1.79.2
	github.com/charmbracelet/bubbles v0.20.0
	github.com/charmbracelet/bubbletea v1.2.4
	github.com/charmbracelet/lipgloss v1.0.0
	github.com/coder/websocket v1.8.12
	github.com/go-chi/chi/v5 v5.2.0
	github.com/go-sql-driver/mysql v1.9.2
	github.com/goccy/go-yaml v1.17.1
	github.com/golang-jwt/jwt/v5 v5.2.2
	github.com/google/uuid v1.6.0
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/joho/godotenv v1.5.1
	github.com/lib/pq v1.10.9
	github.com/lrstanley/bubblezone v0.0.0-20250110055121-b45205ce63e2
	github.com/mattn/go-sqlite3 v1.14.24
	github.com/muesli/termenv v0.15.2
	github.com/pkg/errors v0.9.1
	github.com/ponyruntime/tree-sitter-markdown v0.0.2
	github.com/ponyruntime/tree-sitter-sql v0.0.3
	github.com/sergi/go-diff v1.3.1
	github.com/stretchr/testify v1.10.0
	github.com/tmc/langchaingo v0.1.13
	github.com/tree-sitter-grammars/tree-sitter-lua v0.2.0
	github.com/tree-sitter/go-tree-sitter v0.24.0
	github.com/tree-sitter/tree-sitter-c-sharp v0.23.1
	github.com/tree-sitter/tree-sitter-go v0.23.4
	github.com/tree-sitter/tree-sitter-html v0.23.2
	github.com/tree-sitter/tree-sitter-javascript v0.23.1
	github.com/tree-sitter/tree-sitter-php v0.23.11
	github.com/tree-sitter/tree-sitter-python v0.23.6
	github.com/tree-sitter/tree-sitter-typescript v0.23.2
	github.com/wippyai/module-registry-proto-go v0.0.1
	github.com/xuri/excelize/v2 v2.9.0
	github.com/yuin/gopher-lua v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otel v1.36.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.36.0
	go.opentelemetry.io/otel/sdk v1.36.0
	go.opentelemetry.io/otel/trace v1.36.0
	go.temporal.io/api v1.44.1
	go.temporal.io/sdk v1.32.1
	go.uber.org/mock v0.5.2
	go.uber.org/zap v1.27.0
	golang.org/x/crypto v0.38.0
	golang.org/x/time v0.5.0
	gopkg.in/yaml.v3 v3.0.1
)

// replace github.com/ponyruntime/tree-sitter-sql => ../tree-sitter-sql

replace github.com/yuin/gopher-lua => github.com/ponyruntime/go-lua v0.0.0-20250409012022-15ae569d11fd

//replace github.com/yuin/gopher-lua => ../go-lua

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.6-20250425153114-8976f5be98c1.1 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/CloudyKit/fastprinter v0.0.0-20200109182630-33d98a066a53 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.10 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.17.67 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.34 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.7.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.18.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.25.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.30.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.33.19 // indirect
	github.com/aws/smithy-go v1.22.2 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.2 // indirect
	github.com/charmbracelet/harmonica v0.2.0 // indirect
	github.com/charmbracelet/x/ansi v0.7.0 // indirect
	github.com/charmbracelet/x/term v0.2.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dlclark/regexp2 v1.10.0 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.3 // indirect
	github.com/lann/builder v0.0.0-20180802200727-47ae307949d0 // indirect
	github.com/lann/ps v0.0.0-20150810152359-62de8c46ede0 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/pkoukk/tiktoken-go v0.1.6 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/ponyruntime/go-lua v1.2.1 // indirect
	github.com/richardlehane/mscfb v1.0.4 // indirect
	github.com/richardlehane/msoleps v1.0.4 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/sahilm/fuzzy v0.1.1 // indirect
	github.com/xuri/efp v0.0.0-20240408161823-9ad904a10d6d // indirect
	github.com/xuri/nfp v0.0.0-20240318013403-ab9948c2c4a7 // indirect
	gitlab.com/golang-commonmark/html v0.0.0-20191124015941-a22733972181 // indirect
	gitlab.com/golang-commonmark/linkify v0.0.0-20191026162114-a0c2df6c8f82 // indirect
	gitlab.com/golang-commonmark/markdown v0.0.0-20211110145824-bf3e522c626a // indirect
	gitlab.com/golang-commonmark/mdurl v0.0.0-20191124015652-932350d1cb84 // indirect
	gitlab.com/golang-commonmark/puny v0.0.0-20191124015043-9f83538fa04f // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.36.0 // indirect
	go.opentelemetry.io/otel/metric v1.36.0 // indirect
	go.opentelemetry.io/proto/otlp v1.6.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/mod v0.18.0 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/sync v0.14.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	golang.org/x/tools v0.22.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250519155744-55703ea1f237 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250519155744-55703ea1f237 // indirect
	google.golang.org/grpc v1.72.1 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

tool go.uber.org/mock/mockgen
