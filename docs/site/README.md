# Site da documentação MHL

Este diretório contém uma versão estática do manual da linguagem, pronta para publicação no GitHub Pages.

## Publicação

Configure o GitHub Pages para publicar o diretório `docs/site`, ou use uma GitHub Action de Pages com esse diretório como artefato. O site não depende de build nem de backend.

O destaque de sintaxe é implementado em `app.js`, portanto funciona para MHL mesmo que o GitHub não reconheça `mhl` como uma linguagem nativa em blocos Markdown.
