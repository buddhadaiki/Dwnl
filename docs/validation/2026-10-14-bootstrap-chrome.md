# Bootstrap privado e primeira validação Chrome — 14/10/2026

O setup foi compilado, mas nunca executado. O fluxo abaixo é o plano revisável para uma futura primeira instalação, que ainda depende de autorização específica. A leitura no contexto normal do usuário confirmou que `C:\Users\buddhadaiki\AppData\Local\Dwnl`, o manifesto, o registro nativo e a chave de desinstalação Dwnl não existiam. O readback inicial em AppContainer representava seu diretório/registro virtualizados e não é usado como baseline da máquina.

## Alterações previstas pelo setup

O instalador usa modo por usuário (`PrivilegesRequired=lowest`), pasta fixa `%LOCALAPPDATA%\Dwnl` e arquitetura x64. O [Inno documenta esse modo](https://jrsoftware.org/ishelp/topic_admininstallmode.htm). Não solicita elevação.

| Destino | Conteúdo previsto |
|---|---|
| `%LOCALAPPDATA%\Dwnl` | `dwnl-host.exe`, `suppliers.lock.json`, `THIRD_PARTY_NOTICES.md`, `native-host.json` e arquivos `uninsNNN.exe/.dat` do Inno. |
| `Dwnl\bin` | `yt-dlp.exe`, `deno.exe`, `ffmpeg.exe` e `ffprobe.exe`, adquiridos e conferidos pelos pins. |
| `Dwnl\licenses` | Nove avisos embutidos e o LICENSE/README selecionado do arquivo Gyan. |
| Temporários do setup | Três artefatos baixados, subpastas de extração e log `Setup Log ... .txt` em `%TEMP%`. |
| `HKCU\Software\Google\Chrome\NativeMessagingHosts\com.dwnl.host` | Valor padrão REG_SZ com o caminho absoluto de `native-host.json`. |
| `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\{B01F7A32-74CA-4C9E-BDD2-635B305A5480}_is1` | Metadados do desinstalador gerenciados pelo Inno. [AppId](https://jrsoftware.org/ishelp/topic_setup_appid.htm) determina essa chave. |

As três aquisições totalizam **175.238.696 bytes**, além do overhead de rede: yt-dlp 2026.08.19, Deno 2.9.7 e Gyan FFmpeg 9.0.2 Essentials. São URLs de releases fixas de seus repositórios oficiais no GitHub; redirecionamentos de entrega do fornecedor podem ocorrer. O setup não acessa plataformas de vídeo e não executa os quatro binários de mídia. Ele embute somente nosso helper e avisos.

O manifesto autoriza exclusivamente `chrome-extension://lphdonkmiecidfgjfplhdfnpphmdioeh/`, aponta para o helper privado e usa transporte stdio. O [Chrome documenta a chave HKCU e `allowed_origins`](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging). Esse registro é por usuário Windows; Developer Mode e carregamento da extensão são ações no perfil Chrome escolhido.

O setup não modifica HKLM, PATH, serviços, autostart ou perfil Chrome. Ele não carrega a extensão. `completeCorrespondingSource=false` e `publicDistributionReady=false` continuam registrados; esta aquisição direta não resolve o gate do bundle antigo.

## Sequência e limites verificados

Os guards recusam pasta/registro sem ownership ou arquivos ativos. A aquisição e os hashes externos/internos precedem a cópia. Depois da cópia, os hashes instalados são conferidos antes de salvar o manifesto e escrever HKCU. Valor padrão não-string existente também é recusado.

Foi adicionada compensação limitada para a primeira instalação: se a escrita do registro nativo falhar após salvar nosso manifesto, o código remove somente o manifesto de bytes esperados e eventual valor próprio, preserva substituições de terceiros e não apaga uma instalação anteriormente reconhecida. A revisão estática e a compilação verificam esse ramo; sua execução e o rollback completo de outras falhas pós-cópia **não foram validados**. Uma falha real exige guardar o log e conferir o estado antes de tentar de novo.

O desinstalador remove arquivos conhecidos somente quando ainda correspondem aos hashes e remove manifesto/registro nativo somente quando ambos continuam próprios. Preserva os downloads, a extensão e o estado do usuário. **Limitação conhecida:** depois de uso e desinstalação, `Dwnl\state` pode manter a pasta sem o manifesto/registro, e o guard atual recusará a reinstalação nessa pasta. Não há fluxo de reparo/reinstalação validado; isso não foi contornado com exclusão de estado nem novo marcador persistente.

## Ações manuais futuras no Chrome

1. Após autorização específica, executar o **setup final identificado por SHA256**. Ele é unsigned; não há autorização para desativar proteções do Windows ou Chrome. Guardar o log e conferir os quatro pins instalados, manifesto, HKCU e desinstalador.
2. No perfil Chrome escolhido, abrir `chrome://extensions`, habilitar Developer Mode e usar **Load unpacked** na pasta do build `dist/extension` ou na raiz extraída do ZIP da extensão. Não selecionar o ZIP nem a pasta TypeScript de fontes. O [procedimento oficial](https://developer.chrome.com/docs/extensions/get-started/tutorial/hello-world) usa uma pasta que contém `manifest.json`.
3. Conferir o ID `lphdonkmiecidfgjfplhdfnpphmdioeh`, abrir/fixar o popup e confirmar o handshake Native Messaging e as capacidades reais. O handshake pode criar `%LOCALAPPDATA%\Dwnl\state`; o primeiro download pode criar `%USERPROFILE%\Downloads\Dwnl`. Preferências, histórico/journal e temporários seguem o fluxo do host/extensão, sem serviço ou retomada automática.
4. Para o primeiro E2E, usar o mesmo reel público Instagram já validado, baixar seu vídeo e conferir arquivo/ffprobe e histórico após reabrir o popup. MP3 desse reel não se aplica, pois a amostra física não tem áudio. O X com áudio/MP3 e cancelamento podem ser casos seguintes, com escopo explicitamente autorizado.

O recurso do setup no popup é privado ao build; uma falha ao copiá-lo permite executar manualmente `installer/Dwnl-Setup.exe` da pasta extraída, sob a mesma autorização. O setup não depende da extensão já carregada.

## Decisão mínima pendente

Autorizar **uma primeira instalação do setup final**, as três aquisições oficiais, os arquivos/duas chaves HKCU acima e **Developer Mode + Load unpacked no perfil Chrome escolhido**, seguido de handshake e um download Instagram com estado/destino padrão. O próximo passo precisa identificar o perfil e o SHA256 exato; nenhum desses atos foi executado nesta etapa.
