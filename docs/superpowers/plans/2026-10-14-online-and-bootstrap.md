# Extração pública e aquisição oficial — plano de fechamento

> **For agentic workers:** Execute inline com root como implementador e um revisor independente, preservando a coordenação já escolhida. O pedido do coordenador de 14/10 autoriza implementar estas lacunas; não exige nova escolha do usuário sobre transporte ou aquisição online.

**Goal:** executar análise/download públicos reais das quatro plataformas e compilar um setup que adquira dependências diretamente dos fornecedores com hashes fixos.

**Architecture:** o host materializa seu adaptador privado embutido e invoca exclusivamente a IE Dwnl. Ela chama apenas extratores nativos autorizados e registra um único transporte HTTPS que valida host/DNS, conecta ao IP validado, verifica TLS e controla redirecionamentos/limites. Não há proxy, porta, servidor, serviço ou driver. O setup baixa arquivos oficiais em staging antes de instalar, verifica hashes externos/internos e inclui apenas nosso host/adaptador/avisos no executável distribuído.

**Tech Stack:** Go existente, yt-dlp 2026.08.19 e seu Python empacotado, Deno 2.9.7, FFmpeg/ffprobe Gyan 9.0.2, Inno 7.1.0, unittest/vitest e fixtures reais.

**Spec:** [desenho aprovado](../specs/2026-10-13-dwnl-design.md), complementado pela autorização direta do coordenador em 14/10 para resolver as lacunas, sem instalar/registrar.

## Restrições globais

- HTTPS para URLs de entrada conforme especificação; HTTPS de mídia somente hosts documentados e IP público conectado, sem credenciais/ports arbitrários.
- Nenhum cookie de conta, login, netrc, proxy, desafio anti-bot, DRM, geo-spoofing, plugin de usuário ou motor adicional.
- Uma tarefa, processos em Job Object, cancelamento/EOF e finalização física existentes preservados.
- Sem instalação efetiva, registro Windows/Chrome, autostart, publicação ou push nesta etapa.
- Fontes incompletas continuam rotuladas honestamente; aquisição do fornecedor não torna o bundle anterior redistribuível.

## Review Focus

- Plugin faltante/quebrado: a seleção exclusiva Dwnl precisa impedir fallback para extratores padrão.
- Host/DNS/redirect/peer incompatível: bloquear a conexão antes de alcançar rede interna, inclusive mixed DNS/IPv6.
- Download HLS/DASH: não permitir fallback para FFmpeg ou downloader externo com rede não verificada.
- Formato/item mudou: rejeitar seleção, não baixar outro item ou reduzir qualidade silenciosamente.
- Download/setup cancelado ou corrompido: não publicar arquivo final nem registrar instalação parcial; preservar estado anterior.

## T7 — transporte e caminho real

**Files:** criar `host/internal/engine/transport/yt_dlp_plugins/extractor/dwnl.py`, `transport/tests/test_transport.py`, `online.go`, `online_test.go`; alterar `engine.go`, `formats.go`, `ytdlp.go`, `media.go`, `media_windows.go`, `media_other.go` e `host/cmd/dwnl-host/main.go`; complementar integração e relatórios.

**Interfaces:** `Config.PrivateBinDir`, `Config.AnalysisRoot`; `Engine.Analyze` permanece a porta pública existente. `localAnalysis` ganha seleção remota privada por item/formato. Nenhum campo de comando ou URL de CDN entra no protocolo Chrome.

- [x] Cobrir validação, conexão pinada, redirects, limites e loader ausente; preservar os RED efetivamente observados e usar componentes reais quando possível.
- [x] Implementar singleton RH e DwnlIE fail-closed; desativar cookies/geo/challenge, download externo e protocolos não auditados.
- [x] Implementar metadata bounded em temporário privado, escolhas reais e download native por ID exato; mux/MP3/finalização continuam no motor existente.
- [x] Verificar cancelamento e mudança de seleção, codecs e arquivo físico no caminho online controlado.
- [x] Executar casos públicos reais autorizados ou registrar erro real e sua origem (site/executor/política específica), sem contornar acesso.

## T8 — bootstrap de dependências

**Files:** criar `release/suppliers.lock.json`, `tools/bootstrap.ps1`, `installer/bootstrap.iss`, testes `installer/tests/bootstrap.test.ts`; ajustar build/avisos e empacotamento de recurso local da extensão.

**Interfaces:** O entrypoint de `tools/bootstrap.ps1` produz setup real e relatório de modo `supplier-acquisition`; não chama setup, registro ou desinstalação. Lock fixa URLs oficiais, SHA do arquivo, executáveis extraídos e avisos.

- [x] Cobrir staging sem bundle, arquivo alterado, URL flutuante, pin ausente e origem estável; preservar os RED efetivamente observados.
- [x] Gerar payload próprio e preflight de aquisição equivalente ao setup, com ZIPs oficiais/hash antes da extração e SHA de executáveis/avisos selecionados.
- [x] Compilar Inno: downloads no PrepareToInstall, arquivos verificados antes de instalar, manifesto/registro somente depois da cópia e readback finais.
- [x] Validar preflight real no workspace e compilar setup sem executá-lo; incluir o recurso privado da extensão apenas no build explicitamente pedido.
- [x] Revisão independente dos limites e atualização de código/evidências, reutilizando suítes inalteradas.

## Decisão técnica

O adaptador privado é parte do nosso motor empacotado, não um plugin livre exposto ao navegador. Selecionar apenas Dwnl evita que erro de importação do plugin restaure a rede padrão. O transporte mantém os formatos/progresso nativos do yt-dlp e limita protocolos/downloaders auditados, sem introduzir um endpoint local. A aquisição online já foi aceita pelo usuário; o novo modo não embute os grandes executáveis dos fornecedores nem altera o gate do modo de bundle antigo.
