# OpenAI -> Ollama Proxy

Fork of [xsharov/enchanted-ollama-openrouter-proxy](https://github.com/xsharov/enchanted-ollama-openrouter-proxy).

⚠️ **Work in progress 🚧** (see `TODO` comments).

## Usage
You can provide your **OpenRouter** (OpenAI-compatible) API key through an environment variable or a command-line argument:

### 1. Environment Variable
```bash
    # export OPENAI_BASE_URL="https://some-open-ai-api/api/v1/" # Optional. Defaults to https://openrouter.ai/api/v1/
    export OPENAI_API_KEY="your-api-key"
    ./ollama-proxy
```

### 2. Command Line Argument
```bash
    ./ollama-proxy "your-openrouter-api-key"
```
or
```bash
    ./ollama-proxy "https://some-open-ai-api/api/v1/" "your-api-key"
```

Once running, the proxy listens on port `11434`. You can make requests to `http://localhost:11434` with your Ollama-compatible tooling.

## Installation
1. **Clone the Repository**:

       git clone https://github.com/your-username/ollama-openrouter-proxy.git
       cd ollama-openrouter-proxy

2. **Install Dependencies**:

       go mod tidy

3. **Build**:

       go build -o ollama-proxy
