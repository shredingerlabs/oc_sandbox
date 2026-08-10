code generation:
1. glm-4.7 (dominant LiveCodeBench, still strong SWE-bench)
2. deepseek-v4-flash (balanced, strong Aider score)
3. qwen3.6-27b / qwen3-coder-next (solid, purpose-built, good Aider/LiveCodeBench)
4. mistral-medium-3.5-128b (drops here — great at fixing, mediocre at generating)
5. qwen3.5-397b-a17b (still contested/unclear numbers overall)


bugfixing/debugging:
1. deepseek-v4-flash — ~79% (Flash-Max, self-reported)
2. qwen3.5-397b-a17b — ~80% per one tracker, but contested/inconsistent across sources
3. mistral-medium-3.5-128b — 77.6%
4. qwen3.6-27b — 77.2%
5. glm-4.7 — 73.8%
6. qwen3.6-35b-a3b — 73.4% (only 3B active — best efficiency)
7. qwen3.5-122b-a10b — 72.4%
8. devstral-2-123b-instruct-2512 — 72.2%
9. qwen3-coder-next — 70.6% (3B active — great for local opencode.ai)
