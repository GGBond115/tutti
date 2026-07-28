# Model Tier Reference

This is a routing prior adapted from the packaged OpenSquilla provider ladders
at revision `d8652b72` (2026-07-28), plus Tutti-native families visible in this
repository. It is not a benchmark result or an availability catalog.

Always intersect this table with the exact models returned by the current
target's `agent composer-options` command. Never copy an unavailable id from
this file into a plan.

## OpenSquilla ladders

| Provider        | C0                              | C1                                         | C2                                | C3                                         | Vision route                 |
| --------------- | ------------------------------- | ------------------------------------------ | --------------------------------- | ------------------------------------------ | ---------------------------- |
| OpenAI          | `gpt-5.4-nano`                  | `gpt-5.4-mini`                             | `gpt-5.5` medium effort           | `gpt-5.5` high effort                      | Use current catalog evidence |
| OpenRouter      | `deepseek/deepseek-v4-flash`    | `deepseek/deepseek-v4-pro`                 | `z-ai/glm-5.2`                    | `anthropic/claude-opus-4.8`                | `moonshotai/kimi-k2.6`       |
| DashScope       | `qwen3.6-flash`                 | `qwen3.7-plus`                             | `qwen3.7-max`                     | `qwen3.7-max`                              | Use current catalog evidence |
| Qwen Token Plan | `qwen3.6-flash`                 | `qwen3.7-plus`                             | `qwen3.7-max`                     | `qwen3.8-max-preview`                      | `qwen3.7-plus`               |
| DeepSeek        | `deepseek-v4-flash` no thinking | `deepseek-v4-flash` low thinking           | `deepseek-v4-pro` medium thinking | `deepseek-v4-pro` high thinking            | Use current catalog evidence |
| Gemini          | `gemini-3.1-flash-lite`         | `gemini-3.5-flash`                         | `gemini-3.1-pro-preview`          | `gemini-3.1-pro-preview` high thinking     | Use current catalog evidence |
| Zhipu           | `glm-5-turbo`                   | `glm-5`                                    | `glm-5.1`                         | `glm-5.2`                                  | Use current catalog evidence |
| Moonshot        | `kimi-k2.6` low thinking        | `kimi-k2.6` medium thinking                | `kimi-k2.6` medium thinking       | `kimi-k2.7-code`                           | `kimi-k2.6`                  |
| Volcengine      | `doubao-seed-2-0-lite-260215`   | `doubao-seed-2-0-lite-260215` low thinking | `doubao-seed-2-0-pro-260215`      | `doubao-seed-2-0-pro-260215` high thinking | Use current catalog evidence |
| BytePlus        | `seed-2-0-lite-260228`          | same model, low thinking                   | same model, medium thinking       | same model, high thinking                  | Use current catalog evidence |
| TokenRhythm     | `deepseek-v4-flash`             | `deepseek-v4-pro`                          | `kimi-k2.7-code`                  | `glm-5.2`                                  | `kimi-k2.6`                  |

## Tutti-native family additions

Use these only when the exact family is present in current
`composer-options`.

| Family or exact model    | Prior tier | Notes                                                                      |
| ------------------------ | ---------- | -------------------------------------------------------------------------- |
| `gpt-5.6-terra`          | C2         | Balanced agentic coding model for everyday work                            |
| `gpt-5.6-sol`            | C3         | Frontier agentic coding model for the hardest implementation and review    |
| Claude Haiku family      | C0-C1      | Prefer for bounded, latency-sensitive work                                 |
| Claude Sonnet family     | C2         | Prefer for substantial implementation and debugging                        |
| Claude Opus family       | C3         | Prefer for architecture, deep review, and high-stakes synthesis            |
| Gemini Flash-Lite family | C0         | Prefer for the fastest simple work                                         |
| Gemini Flash family      | C1         | Balanced low-latency route                                                 |
| Gemini Pro family        | C2-C3      | Use current description and reasoning controls to distinguish the top tier |

## Family inference

When an exact model is absent from the table:

1. Prefer its current provider description and advertised capabilities.
2. Match a known family only when the family and generation are unambiguous.
3. Treat `nano`, `flash-lite`, and equivalent small variants as C0 priors.
4. Treat `mini`, `flash`, `lite`, and equivalent balanced variants as C1
   priors unless provider evidence states otherwise.
5. Treat `pro`, `max`, `sonnet`, and code-specialized strong variants as C2
   priors.
6. Treat `opus`, frontier, highest-capability, and explicit high-stakes variants
   as C3 priors.
7. Treat `turbo` as a speed signal, not automatically as a lower capability
   tier.
8. Do not infer image support, context size, tool support, or availability from
   the name.
