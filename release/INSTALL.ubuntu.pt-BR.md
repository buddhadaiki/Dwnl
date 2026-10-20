# Dwnl para Ubuntu — instalação por usuário

Versão experimental para **Ubuntu 24.04 ou posterior, x86_64**, com sessão gráfica e **Google Chrome instalado pelo pacote .deb oficial**. Chromium via Snap, Flatpak e navegadores em contêiner não são suportados neste pacote. A extensão usa Native Messaging e precisa do auxiliar instalado no mesmo usuário Linux do Chrome.

## 1. Preparar o Ubuntu

Instale os pré-requisitos pelos repositórios do Ubuntu, se ainda não estiverem disponíveis:

```sh
sudo apt update
sudo apt install python3 ca-certificates ffmpeg zenity
```

Esse comando é manual. O instalador Dwnl nunca executa sudo nem altera pacotes do sistema. FFmpeg e ffprobe são usados em `/usr/bin`; Zenity abre os seletores de pasta na sessão gráfica. Não é necessário instalar Node.js, Go, yt-dlp ou Deno globalmente.

## 2. Instalar o auxiliar

Baixe **`Dwnl-VERSAO-ubuntu-x64.tar.gz`**: ele contém instalador, extensão e estas instruções. Extraia em uma pasta permanente. O ZIP `Dwnl-VERSAO-chrome-ubuntu.zip` é opcional, para quem deseja atualizar apenas a extensão. Compare os arquivos baixados com os hashes correspondentes em `SHA256SUMS.txt` da entrega.

Na pasta extraída `Dwnl-ubuntu`, execute como seu usuário normal, **sem sudo**:

```sh
sh ./install.sh
```

O instalador baixa yt-dlp e Deno de versões oficiais fixadas, verifica SHA-256 e tamanho do download e de cada executável extraído. Todos os downloads e verificações terminam antes de substituir uma instalação anterior. A instalação usa `${XDG_DATA_HOME:-$HOME/.local/share}/Dwnl/runtime`, com executáveis privados em `runtime/bin`. Os fornecedores não são redistribuídos dentro do TAR; a instalação requer internet e certificados HTTPS válidos. Versões, hashes e referências de licenças/fontes constam em `suppliers.linux.lock.json`.

O registro do Google Chrome fica em `${XDG_CONFIG_HOME:-$HOME/.config}/google-chrome/NativeMessagingHosts/com.dwnl.host.json`. Um registro de outra instalação ou arquivos modificados são preservados e causam erro explícito. Feche downloads Dwnl em andamento antes de reinstalar ou desinstalar.

## 3. Carregar a extensão no Chrome

1. Mantenha a pasta `Dwnl-ubuntu` extraída do TAR em uma localização permanente. Se baixou apenas o ZIP da extensão, extraia-o em uma pasta permanente.
2. Abra `chrome://extensions`, habilite **Modo do desenvolvedor** e clique em **Carregar sem compactação**.
3. Selecione a subpasta `extension` do pacote extraído.
4. Confirme o ID `lphdonkmiecidfgjfplhdfnpphmdioeh` e fixe Dwnl na barra do Chrome.
5. Abra a extensão e confira a conexão com o auxiliar. Depois escolha a pasta de destino e teste um conteúdo cujo download você tenha autorização para realizar.

Não apague a pasta da extensão enquanto ela estiver carregada. O auxiliar é iniciado pelo Chrome conforme necessário; não há serviço, daemon, autostart ou integração de sistema a instalar. Para trocar de versão, instale o TAR novo e carregue/recarregue a extensão da mesma versão.

## Remover o auxiliar

```sh
sh "${XDG_DATA_HOME:-$HOME/.local/share}/Dwnl/runtime/uninstall.sh"
```

O desinstalador confere o inventário e remove apenas o runtime pertencente a esta instalação e seu registro Native Messaging. Downloads e estado/configurações do Dwnl são preservados. Arquivos desconhecidos, links ou arquivos alterados interrompem a remoção. Remova a extensão separadamente em `chrome://extensions`.

## Limitações e diagnóstico

- Se o auxiliar não conectar, confira a saída do instalador, o ID da extensão e o caminho `path` no manifesto Native Messaging. Chrome e instalador precisam rodar com o mesmo usuário e os mesmos `XDG_DATA_HOME`/`XDG_CONFIG_HOME` quando personalizados.
- O host e o empacotamento foram testados no Ubuntu 26.04 via WSL. Ainda falta validação visual do Chrome e do seletor Zenity numa sessão desktop, assim como uma execução nativa no Ubuntu 24.04.
- Cancelamento normal encerra o grupo de processos do auxiliar. Se o host for encerrado abruptamente, Linux não oferece aqui a mesma garantia de encerramento de todos os descendentes dos Job Objects do Windows.
- A disponibilidade de downloads depende do site e dos fornecedores. Conteúdo protegido por DRM não é suportado.
- Pacote sem assinatura digital; SHA-256 detecta arquivos diferentes dos anexos publicados, mas pressupõe uma fonte confiável para o arquivo de checksums.
