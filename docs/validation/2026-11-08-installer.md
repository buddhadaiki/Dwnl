# Instalador novo e status da extensão — 08/11/2026

Retorno depois de instalar a 0.4.0:
- o instalador ficou feio e em inglês;
- o aviso "O Dwnl ainda está aberto" não tinha como fechar o Dwnl;
- **Conectar ao Chrome** dizia "Extensão conectada" sem extensão nenhuma no Chrome.

## "Extensão conectada" sem extensão

**Causa:** o app gravava `state\extension-connected` na primeira vez que a extensão o contatava e nunca apagava o arquivo. Na instalação pessoal, o arquivo é de 05/11 às 16:58, de uma conexão antiga. Depois que a extensão saiu do Chrome, o registro continuou valendo para sempre, e nada avisa o app quando ela é removida.

**Correção:**
- **Conectada** passa a significar que a extensão falou com o app desde que ele abriu.
- O arquivo guarda só a data do último contato. Ele é atualizado no primeiro contato de cada execução, com troca atômica.
- Configurações mostram **Conectada.**, **Última conexão em dd/mm/aaaa.** ou **Ainda não conectada.**
- O passo a passo fica em "Aguardando a extensão…" até um contato nesta execução.
- O cartão de convite continua aparecendo só para quem nunca conectou.
- O `dwnl-desktop-check` deixou de contar como extensão.

**Teste de regressão:** `TestOnlyAContactInThisRunCountsAsConnected` simula um registro de uma execução anterior. Ele ficou vermelho com o significado antigo ("qualquer registro = conectada"), por mutação temporária, e verde com a correção. Também há testes do formato do registro, inclusive o arquivo gravado pela 0.3.0, e dos textos na janela.

## Fechar o Dwnl pelo instalador

O aviso agora é um diálogo com três opções:
- **Fechar o Dwnl:**
  1. pede `--quit --force`;
  2. se ainda houver algo rodando, encerra os processos cujo executável está na pasta da instalação. Isso inclui um Dwnl 0.3.0, que não atende ao pedido, a ponte do Chrome com o painel aberto e ferramentas de download que ficaram para trás.
- **Tentar de novo:** para depois de usar **Sair**.
- **Cancelar.**

A busca usa o WMI (`Win32_Process.ExecutablePath`) e compara o caminho com a pasta, com a barra final. Por isso um Dwnl rodando de outra pasta nunca é tocado, nem de uma pasta cujo nome começa igual. Em modo silencioso, o padrão continua sendo cancelar.

**Teste real:** `installer/tests/processes.test.ts` compila `bootstrap-processes-harness.iss`, que usa o mesmo `bootstrap-processes.iss` do instalador.
- Ele inicia quatro processos de espera:
  - `install-資料\Dwnl.exe`;
  - `install-資料\bin\yt-dlp.exe`;
  - `install-資料-other\Dwnl.exe`;
  - `outside\Dwnl.exe`.
- Confere que só os dois primeiros foram encerrados.
- Duas mutações temporárias deixaram o teste vermelho: sem a barra final, a pasta vizinha também foi encerrada; sem o `Terminate`, nada foi encerrado.

## Visual

- **Estilo:** `WizardStyle=modern dynamic windows11`, que segue o tema claro ou escuro do Windows, como o app.
- **Idioma:** português do Brasil (`BrazilianPortuguese.isl`). As mensagens de erro que ainda estavam em inglês foram traduzidas.
- **Imagens:**
  - o ícone do Dwnl no instalador e na lista de aplicativos (`UninstallDisplayIcon`);
  - o logo no canto das páginas;
  - na página final, um painel azul royal (`#4169e1`, o destaque do app) com o logo e o nome em branco, nas versões clara e escura.
- **Origem das imagens:** foram geradas a partir de `docs/branding/dwnl-logo.png` por `tools/installer-art.py` (Pillow). O script recorta o logo, limpa os pontos translúcidos ao redor e reduz com Lanczos para cada tamanho de tela do Inno 7. A saída fica em `installer/assets`.

**Conferido em prévias:** foram compiladas com as mesmas diretivas e textos, sem arquivos, pasta, desinstalador ou registro. As janelas foram capturadas com `PrintWindow`, nos temas claro e escuro, mostrando a página final e o diálogo novo.

## Testes

- Go, `go test -p 1 ./... -count=1`: todos os pacotes passaram.
- `node --test` do modelo do app: 26 passaram.
- `npm run typecheck` passou. Vitest: 223 passaram, incluindo os testes estáticos, de posse e de processos do instalador.
- Ficaram fora 6 testes Vitest:
  - os 3 de integração nativa, que precisam do harness e de uma cópia dos binários e não foram montados desta vez;
  - 3 que dependem de fixtures de fornecedor ausentes nesta máquina.
- `tools/release-desktop.ps1` compilou o instalador sem avisos.

## Não verificado

O instalador real não foi executado sobre a instalação pessoal. O fluxo completo do diálogo dentro do `PrepareToInstall` foi coberto pela compilação, pelo teste estático e pelo teste real da função que encerra os processos.
