# Design:

Three major components:

1. Frontend

- Is a SPA in 'vanilla' js
- See [frontend](../frontend) for additional details

2. Virtual Gallery

- Crawls local filesystem under specified path for media files
- Builds an index of these files so that they may be served to the frontend, without moving them

3. Media Agent

- Inspects files in the the virtual gallery
- Appends metadata to the media using some LLM + context

## Blueprint:

```
  a. Fsnotify crawls local filesystme under scan-root
     for media files

  b. The media index subscribes to watcher changes and
     adds it to storage


┌─Virtual─Gallery───────────────────────────────────┐
│                                                   │
│ ┌─Watcher─────────┐          ┌─Media─Index──────┐ │
│ │  ┌──────────┐   │          │ ┌──────────────┐ │ │
│ │  │ fsnotify │   │          │ │ storage:     │ │ │
│ │  └──────────┘   ├────b.───►│ │  * metadata  │ │ │
│ │        ▲        │          │ │  * location  │ │ │
│ │        │        │          │ │  * embedding │ │ │
│ │        a.       │          │ └──────────────┘ │ │
│ │        │        │          │ ┌─http-handler─┐ │ │
│ │        ▼        │          │ │   /gallery   │ │ │
│ │ ┌─<scan-root>─┐ │          │ └──────────────┘ │ │
│ │ │ media-files │ │          └───────────┬──────┘ │
│ │ └─────────────┘ │                      │    ▲   │
│ └─────────────────┘                      │    │   │
└──────────────────────────────────────────┼────┼───┘
                                           │    │
  c. Media Agent subscribes to new items   c.   d.
     and adds embeddings + some metadata   │    │
                                           ▼    │
  d. On /suggestion, constructs          ┌─Media┴Agent──────┐
     embedding from user input and       │                  │
     finds closest match from index      │ ┌─http-handler─┐ │
                                         │ │ /suggestion  │ │
                                         │ └──────────────┘ │
                                         └──────────────────┘
```
