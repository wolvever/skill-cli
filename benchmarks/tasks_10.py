"""Expanded public pilot, authored before evaluating the new ranker.
Gold IDs are manually chosen from server contracts, never derived from scores.
This is an author-labeled development corpus, not an independent holdout.
"""
from tasks import TASKS as ORIGINAL

ROWS=[
 ('ctx-resolve','natural','Resolve a programming package name to a Context7 library identifier.','Context7 library identifier',['context7.resolve-library-id']),
 ('ctx-query','natural','Retrieve current Context7 library documentation and code examples when the library ID is already known.','Context7 documentation examples',['context7.query-docs']),
 ('grep-code','natural','Find literal source code patterns across public GitHub repositories using grep.app.','literal code patterns',['grep.searchGitHub']),
 ('sv-docs','natural','Read the full SvelteKit documentation section for routing.','SvelteKit documentation section',['svelte.get-documentation']),
 ('sv-sections','natural','Browse the Svelte documentation section catalog and its use cases before choosing a section.','Svelte sections use cases',['svelte.list-sections']),
 ('sv-playground','natural','Generate a Svelte Playground link from my component code.','Svelte playground link',['svelte.playground-link']),
 ('sv-fix','natural','Analyze a Svelte component and suggest fixes for its issues.','Svelte component fixes',['svelte.svelte-autofixer']),
 ('nx-blog','natural','Read a complete Nuxt blog post when I already know its exact path.','Nuxt full blog post',['nuxt.get-blog-post']),
 ('nx-changelog','natural','Show the latest Nuxt core and official module releases and changelog.','Nuxt releases changelog',['nuxt.get-changelog']),
 ('nx-host','natural','Retrieve the Nuxt deployment setup guide for a known hosting provider.','Nuxt specific hosting guide',['nuxt.get-deploy-provider']),
 ('nx-page','natural','Fetch one Nuxt documentation page using its exact path.','Nuxt documentation page path',['nuxt.get-documentation-page']),
 ('nx-start','natural','Get the official Nuxt Getting Started introduction for a major version.','Nuxt Getting Started',['nuxt.get-getting-started-guide']),
 ('nx-module','natural','Read compatibility, README and stats for a Nuxt module whose identifier is known.','Nuxt module README compatibility',['nuxt.get-module']),
 ('nx-person','natural','Get one Nuxt contributor rank, score and contributions by GitHub username.','Nuxt contributor username contributions',['nuxt.get-nuxter']),
 ('nx-blogs','natural','Browse Nuxt blog post titles, dates and tags to discover recent announcements.','Nuxt blog titles dates',['nuxt.list-blog-posts']),
 ('nx-hosts','natural','Compare the available hosting and deployment providers for Nuxt applications.','Nuxt hosting providers compare',['nuxt.list-deploy-providers']),
 ('nx-pages','natural','List Nuxt documentation page titles and paths for a version.','Nuxt list documentation pages',['nuxt.list-documentation-pages']),
 ('nx-modules','natural','Discover Nuxt authentication modules with filtering and sorting.','Nuxt discover modules filtering',['nuxt.list-modules']),
 ('nx-people','natural','Rank Nuxt ecosystem contributors by score over a period.','Nuxt rank contributors period',['nuxt.list-nuxters']),
 ('nx-search','natural','Search Nuxt documentation for an explanation of server rendering.','Nuxt documentation search',['nuxt.search-documentation']),
 ('astro-docs','natural','Search the official Astro framework documentation.','Astro framework docs',['astro.search_astro_docs']),
 ('hf-files','natural','List and read Hugging Face repository files through filesystem operations.','Hugging Face repository filesystem',['huggingface.hf_fs']),
 ('hf-identity','natural','Inspect my Hugging Face authentication context and visible organization memberships.','Hugging Face authentication context',['huggingface.hf_whoami']),
 ('hf-details','natural','Fetch Hugging Face repository metadata, license and dataset splits for known repo IDs.','Hugging Face repo details splits',['huggingface.hub_repo_details']),
 ('hf-search','natural','Search and sort Hugging Face models, datasets and Spaces through the shared repository query interface.','Hugging Face models datasets search',['huggingface.hub_repo_search']),
 ('cb-post','natural','Get the complete HTML of a Cloudflare Blog post from its slug.','Cloudflare Blog post HTML slug',['cloudflare_blog.get_post']),
 ('cb-posts','natural','List Cloudflare Blog posts in reverse chronological order with cursor pagination.','Cloudflare Blog posts chronological cursor',['cloudflare_blog.list_posts']),
 ('cb-tags','natural','List topic tag slugs used by the Cloudflare Blog.','Cloudflare Blog tags slugs',['cloudflare_blog.list_tags']),
 ('cb-search','natural','Search Cloudflare Blog articles about DDoS protection.','Cloudflare Blog search articles',['cloudflare_blog.search_posts']),
 ('ctx-exact','exact','Select Context7 resolve-library-id.','resolve-library-id',['context7.resolve-library-id']),
 ('grep-exact','exact','Select grep.searchGitHub.','grep.searchGitHub',['grep.searchGitHub']),
 ('sv-exact','exact','Select svelte.svelte-autofixer.','svelte.svelte-autofixer',['svelte.svelte-autofixer']),
 ('nx-exact','exact','Select nuxt.search-documentation.','nuxt.search-documentation',['nuxt.search-documentation']),
 ('astro-exact','exact','Select astro.search_astro_docs.','astro.search_astro_docs',['astro.search_astro_docs']),
 ('hf-exact','exact','Select huggingface.hub_repo_search.','huggingface.hub_repo_search',['huggingface.hub_repo_search']),
 ('cb-exact','exact','Select cloudflare_blog.list_tags.','cloudflare_blog.list_tags',['cloudflare_blog.list_tags']),
 ('none-sms','negative','Send an SMS message to a telephone number. Return no capability if unavailable.','SMS message',[]),
 ('none-transfer','negative','Transfer money between bank accounts. Return no capability if unavailable.','bank transfer',[]),
 ('none-vector','negative','Insert an embedding vector into a Pinecone database. Return no capability if unavailable.','Pinecone vector insert',[]),
 ('none-ticket','negative','Purchase an airline ticket and charge the fare. Return no capability if unavailable.','airline ticket purchase',[]),
]
TASKS=ORIGINAL+[dict(id=i,group=g,prompt=p,query=q,gold_tools=gold) for i,g,p,q,gold in ROWS]
