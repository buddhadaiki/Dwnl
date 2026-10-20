# Dwnl: aplicativo Windows com extensão auxiliar

Data: 2026-10-19. Estado: implementação autorizada pelo usuário, incluindo dependências; evidências em docs/validation/2026-10-19-desktop.md.

## Decisões do usuário

- Windows primeiro; preservar código e suporte Ubuntu existentes.
- Aplicativo completo e painel rápido na extensão.
- Fechar a janela mantém o aplicativo na bandeja e os downloads em execução. Sair explicitamente encerra o aplicativo de forma controlada.
- Inicialização com Windows opcional e desligada por padrão.
- Fila persistente com um download ativo por vez.
- Após falha ou reinício, preservar a fila e aguardar o comando Continuar fila.
- Retomar transferência parcial somente quando tecnicamente suportado e seguro; informar quando exigir reinício do download.
- Vídeos individuais e variações das plataformas atuais: YouTube/Shorts, Instagram/Reels, TikTok e X. Playlists e canais inteiros ficam fora desta etapa.
- Rapidez e baixo consumo são prioridades do produto.
- Identidade visual dark, com azul royal como destaque.

## Desenho proposto

O núcleo Go existente passa a ser executado pelo aplicativo, independentemente do Chrome. O aplicativo será a fonte de verdade para tarefas, fila, histórico, preferências e estado de recuperação. Reutilizar motor, regras de seleção de mídia, validação de arquivos e fronteiras de segurança existentes.

A extensão mantém ações rápidas: obter a aba por gesto explícito, colar URL, analisar, escolher vídeo/MP3 e adicionar à fila. Ela exibe estado fornecido pelo aplicativo e oferece abrir a janela completa. Fechar o navegador não cancela downloads do aplicativo.

A ponte Native Messaging permanece pequena: inicia/localiza uma única instância do aplicativo e transmite comandos por IPC local restrito ao usuário. A implementação deve verificar identidade/origem e impedir comando duplicado de criar duas tarefas. Não abrir porta de rede para a comunicação local.

A janela usa frontend TypeScript/HTML/CSS e WebView2 no Windows, reaproveitando o núcleo Go. Wails v2 é o candidato inicial para integração; fixar uma versão estável compatível e suas dependências no projeto durante a implementação. Não usar versão beta nem instalar ferramentas globalmente. A arquitetura será validada com build e ciclo de vida reais antes de ampliar a interface. WebView2 é um requisito de runtime; detectar sua presença e tratar ausência no instalador com informação explícita ao usuário.

A bandeja precisa funcionar mesmo com a janela fechada. Avaliar descarregar a interface quando oculta se as medições mostrarem ganho material, preservando sempre o núcleo e a fila. Uma segunda abertura deve reutilizar a mesma instância.

Separar conectividade de preparação: responder rapidamente que o app está acessível e informar quais capacidades ainda estão sendo verificadas. Verificações de dependências têm deadline e cancelamento próprios. Aproveitar resultados por identidade/versão instalada sem omitir validações de integridade necessárias.

A fila persistida não deve depender do storage do Chrome. Estados incluem aguardando, analisando, baixando, processando, concluído, cancelado, falhou e interrompido. Recuperação não reinicia trabalho silenciosamente. Preservar preferências e histórico legados; reconciliar cópias sem duplicar downloads e sem descartar estados divergentes.

## Evidências observadas

- host/cmd/dwnl-host/main.go:72-76 bloqueia a resposta inicial em media.Probe.
- host/internal/engine/ytdlp.go:113-185 executa sequencialmente quatro consultas de versão, ffmpeg -encoders e um self-test adicional do adaptador yt-dlp.
- extension/src/controller.ts:35 vence a requisição sem desconectar o canal; transportFailure em 148-153 não troca o estado connecting no timeout. popup.ts:61 esconde a recuperação enquanto connecting. O caminho foi confirmado por inspeção; falta reprodução automatizada/dinâmica.
- O encerramento da conexão cancela a sessão e os jobs do host atual. Esse comportamento pertencia ao desenho anterior e precisa mudar para o app.
- Shorts foi relatado pelo usuário como falha. Não foi fornecida uma URL específica; a causa ainda não foi reproduzida. A inspeção inicial não encontrou rejeição explícita do caminho /shorts/ no normalizador Go; não assumir que basta reescrever a URL.

## Critérios de conclusão

1. App inicia e funciona sem extensão; extensão localiza/inicia o app e se reconecta após falha com feedback claro.
2. Fechar popup, janela ou Chrome não cancela tarefa; Sair e cancelamento explícito encerram somente os processos próprios.
3. Fila preserva ordem e estado entre reinícios, executa apenas um download e não duplica tarefas por clique repetido/reconexão.
4. Falha de dependência, timeout, falta de espaço, permissão negada e arquivo existente não geram falso sucesso nem sobrescrita indevida.
5. Shorts/Reels e formatos equivalentes têm regressões de normalização/extração; validar casos públicos reais viáveis e registrar recusas separadamente de bugs. Uma URL bem-sucedida não comprova suporte universal.
6. Histórico e configurações pertencem ao app; migração do estado legado é reversível e verificada.
7. Interface dark com azul royal, estados de conexão/preparação explícitos, navegação por teclado e contraste legível. Preservar a seleção real de qualidade e bitrate.
8. Medir abertura fria/quente, resposta da ponte, memória e CPU ociosa. Alvos iniciais para a máquina de desenvolvimento: confirmação de conexão em até 500 ms com app ativo e janela utilizável em até 3 s na abertura fria, em pelo menos 20 amostras; apresentar mediana/p95. Esses são alvos, não resultados medidos nem garantia para qualquer PC.
9. Medir separadamente núcleo na bandeja, janela/WebView2 e processamento de mídia. Estabelecer orçamento de memória com a primeira medição funcional; não chamar o app de leve só pelo tamanho do executável.
10. Testes TypeScript e Go proporcionais, build Windows, revisão de diff e exercício real de extensão/app, incluindo encerramento do Chrome e recuperação. Não declarar concluído o comportamento visual sem observá-lo.

## Sequência de implementação proposta

1. Reproduzir e corrigir timeout de handshake; instrumentar conexão/probe e adicionar regressão específica. Investigar Shorts com fixtures e caso real viável.
2. Criar núcleo independente, IPC e ponte, instância única, persistência da fila e recuperação.
3. Construir janela, bandeja e painel rápido com a identidade visual escolhida.
4. Integrar instalador, migração e opção de início automático; executar validações de ponta a ponta e medir desempenho.

Dependências locais necessárias ao shell desktop fazem parte da confirmação deste desenho. Publicação, push, assinatura paga e implantação externa não fazem parte desta etapa. Alterar a instalação pessoal será uma etapa própria, após existir build concreto validado.

## Transferência de diretório executada

Destino físico: C:\Users\buddhadaiki\Desktop\Pastas\Projetos\Dwnl.
Checkout de desenvolvimento: .worktrees\dwnl-prototype.
O diretório antigo está em uso e foi preservado como contêiner de junctions para .git, .worktrees e docs. Isso mantém os caminhos existentes sem duplicar os dados.
Git worktree repair e git fsck --full concluíram. Estados locais principal/worktree foram comparados antes/depois. Foram conferidos 184 hashes de fontes/documentos e o inventário de 40.515 arquivos de trabalho/documentação, além da integridade dos objetos Git. Nenhum commit, branch ou push foi criado.

## Referência técnica

Wails v2, integração Windows e requisito WebView2: https://v2.wails.io/docs/guides/windows/ (consultado em 2026-10-19).