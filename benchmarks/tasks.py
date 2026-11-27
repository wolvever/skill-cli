"""Manually labeled capability-selection tasks. Query is a prescribed retrieval
probe, never disclosed to the model runner. Gold labels are not derived from search.
Tasks ask for capability selection; they do not authorize public-server writes.
"""

# id, group, natural-language request, prescribed search query, required tool IDs
ROWS = [
 ('dw-structure-exact','exact','Select the DeepWiki operation that lists documentation topics for facebook/react.','read_wiki_structure',['deepwiki.read_wiki_structure']),
 ('dw-content-exact','exact','Select the DeepWiki operation that reads the full wiki for facebook/react.','read_wiki_contents',['deepwiki.read_wiki_contents']),
 ('dw-question-exact','exact','Select the DeepWiki operation that answers a codebase question about vercel/next.js.','ask_wiki_question',['deepwiki.ask_wiki_question']),
 ('ms-docs-exact','exact','Select the Microsoft Learn operation to search official Azure Functions documentation.','microsoft_docs_search',['mslearn.microsoft_docs_search']),
 ('ms-fetch-exact','exact','Select the Microsoft Learn operation that fetches the complete article at a supplied Learn URL.','microsoft_docs_fetch',['mslearn.microsoft_docs_fetch']),
 ('ms-code-exact','exact','Select the Microsoft Learn operation that finds Python BlobServiceClient code examples.','microsoft_code_sample_search',['mslearn.microsoft_code_sample_search']),
 ('cf-search-exact','exact','Select the Cloudflare operation to search official Workers documentation.','search_cloudflare_documentation',['cloudflare.search_cloudflare_documentation']),
 ('cf-migrate-exact','exact','Select the Cloudflare operation that supplies the Pages to Workers migration guide.','migrate_pages_to_workers_guide',['cloudflare.migrate_pages_to_workers_guide']),
 ('pet-id-exact','exact','Select the Petstore operation that fetches pet 42 by its ID.','getPetById',['petstore.getPetById']),
 ('pet-status-exact','exact','Select the Petstore operation that lists available pets.','findPetsByStatus',['petstore.findPetsByStatus']),
 ('pet-inventory-exact','exact','Select the Petstore operation that returns stock counts by status.','getInventory',['petstore.getInventory']),
 ('pet-order-exact','exact','Select the Petstore operation that looks up order 7 by its ID.','getOrderById',['petstore.getOrderById']),
 ('pet-user-exact','exact','Select the Petstore operation that looks up the user named user1.','getUserByName',['petstore.getUserByName']),
 ('guru-metrics-exact','exact','Select the APIs.guru operation for basic catalog statistics.','getMetrics',['apisguru.getMetrics']),
 ('guru-providers-exact','exact','Select the APIs.guru operation that lists API providers.','getProviders',['apisguru.getProviders']),
 ('guru-provider-exact','exact','Select the APIs.guru operation that lists APIs from the provider googleapis.com.','getProvider',['apisguru.getProvider']),
 ('dw-topics-paraphrase','paraphrase','I need a table of contents for the React repository knowledge pages. Which DeepWiki capability fits?','table of contents',['deepwiki.read_wiki_structure']),
 ('dw-corpus-paraphrase','paraphrase','Retrieve the whole indexed repository knowledge document from DeepWiki.','whole indexed repository',['deepwiki.read_wiki_contents']),
 ('dw-explain-paraphrase','paraphrase','Ask a repository-aware assistant to explain how Next.js routing is implemented.','explain routing',['deepwiki.ask_wiki_question']),
 ('ms-guide-paraphrase','paraphrase','Find authoritative guidance about Azure Functions execution time limits.','execution time limits',['mslearn.microsoft_docs_search']),
 ('ms-article-paraphrase','paraphrase','Download an entire official Learn article as readable text from its URL.','entire article',['mslearn.microsoft_docs_fetch']),
 ('ms-snippet-keyword','keyword','Find an official Python Azure Blob Storage code sample.','code',['mslearn.microsoft_code_sample_search']),
 ('cf-workflows-keyword','keyword','Find reference material about Cloudflare Workflows.','Workflows',['cloudflare.search_cloudflare_documentation']),
 ('cf-migration-keyword','keyword','Get the mandatory guide for moving Cloudflare Pages projects onto Workers.','guide',['cloudflare.migrate_pages_to_workers_guide']),
 ('pet-lookup-paraphrase','paraphrase','Read an animal record when I already know its numeric identifier.','animal record',['petstore.getPetById']),
 ('pet-stock-paraphrase','paraphrase','How many animals are stocked in each state? Select the inventory capability.','stocked',['petstore.getInventory']),
 ('pet-status-keyword','keyword','List pets that are pending adoption using their status.','status',['petstore.findPetsByStatus']),
 ('guru-stats-paraphrase','paraphrase','Which capability reports the directory size and specification counts?','directory size',['apisguru.getMetrics']),
 ('guru-services-keyword','keyword','List services offered by an APIs.guru provider.','services',['apisguru.getServices']),
 ('dual-doc-search','multi','Select both official documentation search capabilities: Microsoft Learn and Cloudflare.','search',['mslearn.microsoft_docs_search','cloudflare.search_cloudflare_documentation']),
 ('dual-pet-lookup','multi','Select both capabilities needed to look up one Petstore pet and one Petstore order by their IDs.','ID',['petstore.getPetById','petstore.getOrderById']),
 ('dual-guru','multi','Select both APIs.guru capabilities for basic metrics and a list of providers.','Get',['apisguru.getMetrics','apisguru.getProviders']),
 ('dual-wiki','multi','Select both DeepWiki capabilities for a repository topic outline and its complete wiki text.','documentation',['deepwiki.read_wiki_structure','deepwiki.read_wiki_contents']),
 ('none-email','negative','Select a capability that sends a Gmail email. If none can send email, return an empty selection.','send email',[]),
 ('none-charge','negative','Select a capability that charges a Stripe credit card. If none can charge cards, return an empty selection.','charge',[]),
 ('none-delete','negative','Select a capability that deletes a GitHub repository. If none can delete repositories, return an empty selection.','delete',[]),
 ('none-deploy','negative','Select a capability that deploys a Cloudflare Worker, not one that searches documentation about deployment. If unavailable, return an empty selection.','Workers',[]),
 ('none-forecast','negative','Select an operation that returns a weather forecast. If unavailable, return an empty selection.','forecast',[]),
 ('none-calendar','negative','Select an operation that creates a calendar event. If unavailable, return an empty selection.','calendar',[]),
 ('none-schema-drift','negative','Select the obsolete DeepWiki ask_question tool by that exact name. If only a renamed replacement is listed, return an empty selection.','ask_question',[]),
]

TASKS=[dict(id=i,group=g,prompt=p,query=q,gold_tools=gold) for i,g,p,q,gold in ROWS]
