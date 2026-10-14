# Revisão independente — online e bootstrap

Revisor: contracts_review. Somente leitura, sem testes, downloads, setup ou arquivos alterados pelo revisor. Root executou as verificações de implementação.

O revisor encontrou HTTPS/HTTP, codecs desconhecidos, leitura integral de manifestos, troca de cliente YouTube e orçamento cumulativo entre formatos. Root corrigiu os pontos e apresentou regressões. O readback final confirmou as correções, áudio desconhecido com confirmação física/NO_AUDIO, orçamento restante antes da mídia, preservação emCancelFailed, transporte exclusivo e probe offline.

Bootstrap: revisão confirmou três URLs/pins fixos, hashes internos antes de instalar, readback dos arquivos instalados antes do manifesto/HKCU, progresso/cancelamento e finallyHide. Resultado final: aprovado para commit local, nenhum achado bloqueador restante no escopo. Recompilação final e testes continuam sob responsabilidade de root.

Essa aprovação não valida execução de setup/Chrome ou download universal. Fonte completa e distribuição pública continuam desabilitadas.

## Download real, áudio e registro — readback adicional

Revisor único `contracts_review`, somente leitura. A revisão apontou dois P2 do fluxo de setup: manifesto deixado após falha no registro da primeira instalação e reinstalação recusada quando a desinstalação preserva estado. Root implementou compensação limitada de manifesto/valor próprios e rejeição de valor padrão não-string. O revisor confirmou esses ramos sem bloqueador novo. O limite de reinstalação com estado preservado continua explicitamente documentado; não há claim de reparo, rollback completo ou execução real de setup.

A revisão confirmou fallback ABR→TBR somente para áudio-only e concordância entre opção pública e seleção privada da melhor fonte de áudio. Um P3 adicional mostrava o tamanho original como exato após substituir o áudio de um formato progressivo. Root reproduziu o defeito em regressão RED, limpou o tamanho quando a fonte de áudio muda e obteve GREEN para os casos de tamanho exato/estimado.

A regressão Go completa anterior encontrou somente falha de `testing.TempDir` na limpeza da fixture de substituição, sem falha nas assertions de preservação. Após a mesma ocorrência em áudio-only, o revisor aprovou usar o helper NTFS existente em todas as chamadas de diretórios das fixtures de mídia/publicação/online e nas duas de `directMedia` em preparação. Ele só repete `ERROR_DIR_NOT_EMPTY` por até dois segundos, com erro persistente/diferente ainda visível; nenhuma lógica de produção ou assertion alterada. O readback final confirmou os imports e o P3 de tamanho fechado, sem achados novos.

Os resultados finais de testes, compilação e readback físico são responsabilidade de root e estão na validação. A revisão não rodou testes nem alterou arquivos, registro, Chrome ou setup.
