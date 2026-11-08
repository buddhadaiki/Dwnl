O Dwnl 0.4.1 ganha um instalador com o visual do Dwnl, em português e no tema do Windows. Quando o Dwnl continua aberto, o instalador oferece **Fechar o Dwnl**. Esta versão também corrige o status da extensão, que podia dizer "conectada" sem extensão no Chrome.

## Downloads e instalação

- **Recomendado:** `Dwnl-0.4.1-windows-x64.zip`, o pacote completo com instalador, instruções e hashes internos.
- `Dwnl-Setup-0.4.1-windows-x64.exe`, o instalador sozinho. Ele instala o aplicativo e a extensão.
- `LEIA-ME-0.4.1.md`, com instalação, atualização, uso e remoção.
- `SHA256SUMS.txt`, os hashes dos arquivos distribuídos.

Requer Windows x64, internet durante a instalação e o Microsoft Edge WebView2 Runtime Evergreen. O instalador não tem assinatura digital; compare `SHA256SUMS.txt` antes de executar.

**Para atualizar a partir da 0.4.0:** abra **Configurações** e clique em **Buscar atualizações** e depois em **Atualizar**. O Dwnl fecha sozinho, o instalador termina e abre o Dwnl de novo, e a extensão se recarrega quando o painel fecha.

**Para atualizar a partir da 0.3.0:** execute o novo instalador. Quando ele avisar que o Dwnl continua aberto, escolha **Fechar o Dwnl**. Depois, clique em **Recarregar** no cartão do Dwnl em `chrome://extensions`, ou reinicie o Chrome.

Em qualquer caso, a fila, o histórico e as preferências são preservados.

## Novidades

- **Instalador com a cara do Dwnl.**
  - Ele segue o tema claro ou escuro do Windows, como o app, e usa o estilo do Windows 11.
  - Mostra o ícone e o logo do Dwnl, e a página final tem um painel azul com o logo.
  - Agora está todo em português, inclusive as mensagens de erro.
- **Fechar o Dwnl pelo instalador.** Se o Dwnl continuar aberto ao atualizar ou desinstalar, o aviso oferece três opções:
  - **Fechar o Dwnl:** fecha o app e encerra o que ainda roda da pasta do Dwnl, como uma versão antiga ou a conexão do painel aberto no Chrome. Um Dwnl de outra pasta nunca é tocado.
  - **Tentar de novo:** depois de você usar **Sair**.
  - **Cancelar.**

## Correções

- **Status da extensão honesto.** Antes, depois que a extensão se conectava uma vez, o app dizia "Extensão conectada" para sempre, mesmo que ela tivesse sido removida do Chrome. Agora:
  - **Conectada** aparece só quando a extensão falou com o app desde que ele abriu.
  - Antes disso, Configurações mostram a data da última conexão, e o passo a passo fica aguardando a extensão.

## Validação e limites

No Windows 11 x64, passaram:
- a suíte Go completa (`go test -p 1 ./... -count=1`);
- o typecheck;
- os testes do modelo de interface;
- os testes TypeScript, inclusive os de integração nativa.

Ficaram de fora 3 testes, que dependem de arquivos de fornecedor ausentes na máquina de build.

**Status da extensão:** um teste reproduz o falso "conectada" com um registro de uma execução anterior. Ele falha com o comportamento antigo e passa com a correção.

**Fechar o Dwnl:** o mesmo código do instalador foi testado de verdade com processos dentro da pasta da instalação, numa pasta com nome parecido e fora dela. Só os de dentro foram encerrados.

**Visual:** o instalador foi conferido em prévias nos temas claro e escuro. Mais detalhes em `docs/validation/2026-11-08-installer.md`.

O instalador desta publicação foi gerado, mas não executado na máquina de build; a instalação pessoal não foi alterada.

Continuam valendo os limites anteriores:
- um download interrompido recomeça do início;
- playlists e canais não estão incluídos;
- cada download tem limite de 5 GB;
- a instalação em Windows limpo ainda não foi validada.
