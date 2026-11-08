O Dwnl 0.4.0 busca e instala atualizações pelo próprio app, e o instalador de uma versão nova fecha o Dwnl, troca os arquivos e abre o Dwnl de novo.

## Downloads e instalação

- **Recomendado:** `Dwnl-0.4.0-windows-x64.zip`, o pacote completo com o instalador, as instruções e os hashes internos.
- `Dwnl-Setup-0.4.0-windows-x64.exe`, só o instalador. Ele instala o aplicativo e a extensão.
- `LEIA-ME-0.4.0.md`, com instalação, atualização, uso e remoção.
- `SHA256SUMS.txt`, com os hashes dos arquivos distribuídos.

**Requisitos:** Windows x64, internet durante a instalação e o Microsoft Edge WebView2 Runtime Evergreen. O instalador não tem assinatura digital; compare `SHA256SUMS.txt` antes de executar.

**Para atualizar a partir da 0.3.0:**

1. Execute o novo instalador.
2. A 0.3.0 ainda não sabe fechar sozinha. Quando o instalador avisar que o Dwnl continua aberto, use **Sair** no ícone da bandeja e clique em **Repetir**.
3. No fim, o instalador abre o Dwnl.
4. A extensão 0.3.0 não se recarrega sozinha. Clique em **Recarregar** no cartão do Dwnl em `chrome://extensions`, ou reinicie o Chrome.

A fila, o histórico e as preferências são preservados. Dessa versão em diante, atualizar é só clicar em **Atualizar**.

## Novidades

- **Buscar atualizações.** Fica em Configurações e mostra se há uma versão nova, com um link para **Ver novidades**. **Atualizar** baixa o instalador do GitHub, confere o hash dele contra o `SHA256SUMS.txt` e contra o hash que o GitHub publica, e fecha o Dwnl para a instalação começar. Se algo não conferir, nada é instalado. O Dwnl só consulta o GitHub quando você clica.
- **O instalador fecha o Dwnl sozinho.**
  - Com o Dwnl aberto, o instalador pede que ele feche, como no **Sair**, e a fila é salva. Nenhum processo é encerrado à força.
  - Se houver um download em andamento, ele pergunta antes: o download para e volta para a fila.
  - No fim, ele abre o Dwnl de novo. A desinstalação faz o mesmo pedido antes de remover os arquivos.
- **A extensão acompanha o app.** Quando o instalador troca os arquivos da extensão, ela se recarrega sozinha assim que o painel fecha.
- **Mensagens do instalador em português.**

## Validação e limites

**Testes:** no Windows 11 x64, passaram:

- a suíte Go completa (`go test -p 1 ./... -count=1`);
- o typecheck;
- os testes do modelo de interface;
- 223 testes TypeScript, incluindo os de integração nativa.

Outros 3 testes TypeScript dependem de arquivos locais de fornecedor que não estavam na máquina de build.

- **Download do instalador:** as recusas foram testadas com um GitHub local. O instalador é recusado com outros bytes, tamanho errado, hash divergente do `SHA256SUMS.txt` ou do GitHub, e redirecionamento para fora do GitHub.
- **Fechamento pelo instalador:** `Dwnl.exe --quit` foi testado com o app real, aberto na bandeja e com janela. Em ambos ele fechou em cerca de 0,3 s.
- **Detalhes:** `docs/validation/2026-11-07-updates.md`.

**O que não foi testado:**

- O instalador desta publicação foi gerado, mas não executado na máquina de build. A instalação pessoal não foi alterada.
- Por isso, o fluxo completo de instalar por cima de um Dwnl aberto foi coberto só pela compilação e por testes do script.

Continuam valendo os limites anteriores:

- um download interrompido recomeça do início;
- playlists e canais não estão incluídos;
- cada download tem limite de 5 GB;
- a instalação em Windows limpo ainda não foi validada.
