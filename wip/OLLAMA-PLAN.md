# Plano local-first com Ollama (2026-10-30)

Objetivo: terminar tudo o que for possível de [TASKS.md](./TASKS.md) usando
modelos locais no Ollama antes de depender de APIs ou contas externas.

Estado de partida: o provider Ollama já existe
(`server/internal/llm/ollama`, `/api/chat` nativo com tools, `format` e
contagem de tokens), mas não faz streaming e não reporta tokens de cache.
Modelos locais disponíveis: `llama3.2` (rápido, testes) e `qwen3.6:27b`
(fluxos agênticos e tool calls).

Regra geral: o CI continua com modelos determinísticos (mock/fixture). O Ollama
entra como "provider real" em testes opt-in (ex.: `WEFT_TEST_OLLAMA_URL`);
modelos pequenos não devem tornar a suíte instável.

## Fase 0 — Base e Ollama como provider de referência

- Reconciliar itens parcialmente entregues contra o código.
- PROV-04: suíte de conformidade de providers; fixtures para todos e opt-in
  contra o Ollama real (cancelamento, timeout, JSON mode, tools).
- PROV-01: cache-read/cache-write em `llm.Usage`, testado com fixtures.
- PROV-05: apenas design do protocolo de plugins. Ver também a seção
  "Providers como plugins" abaixo.

## Fase 1 — Controle de custo (COST-01..05)

- Totalmente local. Ollama com preço zero por padrão; budgets exercitados com
  um preço fictício cadastrado pelo admin para um modelo local.
- Fecha o teto monetário da seção 8 (linha 687 do TASKS.md).

## Fase 2 — Push e PR pelo gateway (SCM-01, 02, 03, 06 + SCM-04 com fixtures)

- `git.push` contra um repositório bare local como remote.
- Adapter GitHub com servidor fixture `httptest`; live fica para depois.
- Pronto quando: a Software Factory com Ollama termina com push no remote local
  e PR no fixture, depois do gate humano.

## Fase 3 — Gatilhos por issue (TRIG-01..07)

- Jira e GitHub como fixtures (MCP fixture e servidor fixture), como a TRIG-07
  prevê. Depende de SCM-06 (comentário de conclusão com link do PR).

## Fase 4 — Runtime nativo de código (NAT-01..10)

Decisão de 2026-11-10: o OpenCode foi descartado. O runtime nativo do Weft
vira o agente de código, dentro dos gateways, ajustado primeiro para modelos
locais. Design em [docs/native-coding-runtime.md](../docs/native-coding-runtime.md).

Ordem: avaliação (NAT-01) → contexto (NAT-02) → edição e navegação (NAT-03,
NAT-04) → verificação (NAT-05) → plano e checkpoints (NAT-06, NAT-07) →
prompts (NAT-08) → Software Factory v2 (NAT-10). Streaming (NAT-09) depois.
Cada passo só é aceito se a suíte de avaliação melhorar ou se mantiver.

## Fase 5 — Lacunas locais restantes do Release 1

- Proveniência de mudanças de arquivo por execução (linha 191).
- Seção 9 (linhas 708, 712, 713): instalação limpa só pela documentação, com
  Ollama como provider.
- Seção 11: jornada ponta a ponta com Ollama real no lugar do modelo sintético,
  MCPs stdio locais, ambos os modos de aprovação, segredos, isolamento de
  escopo e sandbox em Colima.
- Parte local do LOCAL-07b (Ollama real, VS Code, sandbox real; só macOS arm64).

## Fica para a fase de APIs e contas externas

- PROV-02 (Bedrock), PROV-03 (Gemini).
- SCM-04 live; gatilhos live em Jira/GitHub pessoais.
- SCM-05: Bitbucket DC roda em container local, mas exige licença de avaliação.
- Itens de vendor MCP (linhas 332–407).
- LOCAL-07b com contas reais e outros sistemas operacionais.
- Aceite humano final (linha 972).

Adiados por decisão já registrada: rootless, itens "Later" (incl. AUTH-OIDC),
Release 2.

## Riscos e decisões

- PROV-06 está marcado "depois do PROV-02"; validar o protocolo com um plugin de
  referência baseado no Ollama muda a ordem e precisa de decisão.
- `qwen3.6:27b` (17 GB) com `num_ctx` 32k e tarefas paralelas: memória apertada
  e timeouts prováveis. `llama3.2` para testes rápidos.
- Downloads de modelos, binários ou imagens (Bitbucket) só com
  aprovação prévia.

## Providers como plugins (em discussão)

Pedido em 2026-10-30: providers de inferência como plugins instaláveis,
configuráveis e ativáveis/desativáveis pela interface; por exemplo, um plugin
nativo de Ollama que vem desativado por padrão. Design em discussão antes de
iniciar a Fase 0; ele reordena PROV-04/05/06 e precisa entrar no TASKS.md
quando aprovado.

Modelo proposto: três estados separados — instalado (código disponível),
ativado (admin liberou o tipo), configurado (instâncias com nome, URL,
credencial e modelo). Camada 1: adapters nativos ganham manifesto
(capacidades + schema de configuração que gera o formulário da UI) e estado de
ativação em `llm/plugins`. Camada 2 (PROV-05/06): plugins externos fora de
processo, com checksum, hosts declarados e conformidade antes de ativar.

Decisões (2026-10-30):

- Sem compatibilidade com o estado atual: é uma POC sem usuários. Pode quebrar
  configurações e dados existentes; sem migração.
- Remover as variáveis de ambiente de providers (`OLLAMA_*`, `OPENAI_*`,
  `ANTHROPIC_*`, `OPENROUTER_*`, `HF_*` do Weft, `WEFT_DEFAULT_PROVIDER`)
  e a lista `llm.providers` dos arquivos de configuração. Plugins e instâncias
  são geridos só pela UI/API. Continuam fora da UI: a chave de instalação
  (`WEFT_SECRET_KEY`/keyFile), endereço/diretório de dados e a flag de
  desenvolvimento que expõe o plugin `mock`. Variáveis de teste opt-in (ex.:
  `WEFT_TEST_OLLAMA_URL`) não são configuração de produto e continuam.
- `mock` vira plugin de desenvolvimento, visível só com a flag de dev.
- Sem plugin ativo, "Nova tarefa" fica bloqueada e aponta para a ativação de um
  plugin de inferência.
- Desativar plugin em uso: permitido com confirmação; instâncias viram
  `Unavailable` com motivo claro (recomendação, ainda não confirmada).

## Progresso (2026-11-11)

Branch `local-first-ollama`, um commit por tarefa (autor woliveiras).

- Camada 1 de plugins: PLUG-01..05 concluídas.
- Fase 0: PROV-01 e PROV-04 concluídas (suíte ao vivo passou com Ollama
  `llama3.2`). PROV-05 (design) pendente; PROV-06 aguarda decisão sobre a
  ordem (plugin de referência com Ollama em vez de esperar o Bedrock).
- Fase 1: COST-01..05 concluídas.
- Fase 2: SCM-01..04 e SCM-06 concluídas; SCM-05 (Bitbucket DC) fica para a
  fase externa (licença de avaliação).
- Fase 3: TRIG-01..07 concluídas (com fixtures; contas reais ficam para a
  fase externa).
- Fase 4: replanejada em 2026-11-10 como runtime nativo de código
  (NAT-01..10); o OpenCode foi descartado. NAT-01 a NAT-08 concluídas e a
  NAT-10 entregue (Software Factory v2 semeada e medida na suíte). NAT-09
  (streaming) fica para depois. NAT-04, NAT-05 e NAT-07 foram feitas em
  sessões paralelas, revisadas e mescladas. Medições com `llama3.2` (o qwen
  27B travou a máquina: são 24 GiB de RAM para um modelo de 16,6 GB mais o
  contexto de 32k):
  - linha de base 0/4 com 36 de 45 chamadas falhando;
  - depois das NAT-02/03/06 e do limite de resposta, 0/4 com 8 de 13;
  - depois da coerção de argumentos, 0/4 com 2 de 7.

  A orientação para modelos pequenos manteve 0/4 e fica desligada por padrão.
  A Software Factory v2 resolve 4/4 com o modelo roteirizado e 0/4 com o
  `llama3.2`: os agentes de 3B terminam o turno sem emitir a mensagem
  seguinte.
  Achados:
  - o Ollama gerava respostas sem limite (27k tokens): `num_predict` agora é
    limitado;
  - o Ollama conta o prompt inteiro mesmo quando ele vem do cache.

  Em 2026-11-11 a avaliação passou a usar também modelos em nuvem
  (`make eval-cloud`, Gemini via endpoint compatível com OpenAI, camada
  gratuita). Com o `gemini-3.5-flash`:
  - agente único 4/4: 49 chamadas, 0 erros, cerca de 2 min;
  - Software Factory v2 4/4: 105 chamadas, 0 erros, cerca de 6 min e 2,7
    vezes mais tokens.

  Próximo passo: casos mais difíceis, porque a suíte chegou ao teto.
- Fase 5: jornada nativa opcional com Ollama real passou (plugin, provider,
  tarefa com ferramenta, custo local). Instalação limpa via Compose e aceite
  final dependem de baixar imagens (aprovação).
- Workspace journal follow-up on `main`. Unresolved paths are recorded in
  10-second windows and queue overflows are recorded individually. Segments
  rotate at 64 MiB and retain the cursor chain. SQLite replay, Linux fanotify
  and Chromium journal checks passed. The capacity alert and recovery runbook
  are in place. The local stack upgrade awaits approval. A dedicated workspace
  filesystem is tracked with PROD-08.

Como rodar os checks neste ambiente (sem Go/Node no host):
- Go: `./scripts/go.sh test ./...` (Docker). Falhas pré-existentes:
  `repositoryimport` e `TestRepositoryImportAPIPermissionsAndPersistence`.
- Web: container `node:22` com registry público do npm e `node_modules` em
  volume Docker (o Artifactory corporativo devolve 403).
- Navegador: imagem própria (Playwright 1.64.1 + Go 1.26 + git); a suíte
  nativa também roda nela. `e2e/mcp.spec.ts:18` falha também no `main`.
