/**
 * Shows Browser — fetches /gallery/shows and renders
 * show-grid → season-tabs → episode-list navigation.
 */
(function () {
  const grid = document.getElementById('shows-grid');
  const showCardTpl = document.getElementById('show-card-template');
  const seasonViewTpl = document.getElementById('season-view-template');
  const episodeRowTpl = document.getElementById('episode-row-template');

  let showsData = [];

  function fetchShows() {
    grid.innerHTML = '<div class="loading-state">Loading shows…</div>';

    fetch('/gallery/shows')
      .then(r => {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r.json();
      })
      .then(data => {
        showsData = data.shows || [];
        renderShowGrid();
      })
      .catch(err => {
        console.error('Failed to fetch shows:', err);
        grid.innerHTML = '<div class="loading-state">Failed to load shows. Is the server running?</div>';
      });
  }

  function renderShowGrid() {
    grid.innerHTML = '';

    if (showsData.length === 0) {
      grid.innerHTML = '' +
        '<div class="empty-state">' +
          '<svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">' +
            '<rect x="2" y="3" width="20" height="14" rx="2" ry="2"></rect>' +
            '<line x1="8" y1="21" x2="16" y2="21"></line>' +
            '<line x1="12" y1="17" x2="12" y2="21"></line>' +
          '</svg>' +
          '<p>No shows found</p>' +
          '<p style="font-size:0.85rem;opacity:0.7">Add TV series with S01E02 naming or classification metadata</p>' +
        '</div>';
      return;
    }

    showsData.forEach(function (show) {
      const card = showCardTpl.content.cloneNode(true);
      const title = card.querySelector('.show-card-title');
      const meta = card.querySelector('.show-card-meta');

      const seasonCount = show.seasons.length;
      let totalEpisodes = 0;
      show.seasons.forEach(function (s) {
        totalEpisodes += s.episodes.length;
      });

      title.textContent = show.name;
      meta.textContent = seasonCount + ' season' + (seasonCount !== 1 ? 's' : '') +
        ' · ' + totalEpisodes + ' episode' + (totalEpisodes !== 1 ? 's' : '');

      const cardEl = card.querySelector('.show-card');
      cardEl.addEventListener('click', function () {
        renderSeasonView(show);
      });

      grid.appendChild(card);
    });
  }

  /**
   * Build a human-readable episode display name.
   * Uses metadata 'name' (episode title like "Ethon") if available,
   * otherwise cleans up the filename.
   */
  function episodeDisplayName(ep) {
    // 1. Metadata name (episode title)
    if (ep.Metadata && typeof ep.Metadata === 'object' && ep.Metadata.name) {
      var mn = ep.Metadata.name;
      // Skip if it looks like a raw filename
      if (!/[Ss]\d{1,2}[Ee]\d{1,3}/.test(mn) && !/\d{1,2}x\d{1,3}/i.test(mn)) {
        return mn;
      }
    }
    // 2. Cleaned-up filename
    var raw = ep.Name || '';
    raw = raw.replace(/\.[^.]+$/, '');
    if (ep.ShowName) {
      var escaped = ep.ShowName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      raw = raw.replace(new RegExp('^' + escaped + '[.\\s\\-_]*', 'i'), '');
    }
    raw = raw.replace(/[Ss]\d{1,2}[Ee]\d{1,3}/g, '');
    raw = raw.replace(/\d{1,2}x\d{1,3}/gi, '');
    raw = raw.replace(/[._-]/g, ' ').replace(/\s+/g, ' ').trim();
    if (!raw) raw = ep.Name;
    return raw;
  }

  function renderSeasonView(show) {
    grid.innerHTML = '';
    const view = seasonViewTpl.content.cloneNode(true);

    view.querySelector('.show-title').textContent = show.name;
    view.querySelector('.show-stats').textContent =
      show.seasons.length + ' season' + (show.seasons.length !== 1 ? 's' : '');

    view.querySelector('.back-to-shows-btn').addEventListener('click', function (e) {
      e.stopPropagation();
      renderShowGrid();
    });

    const tabsContainer = view.querySelector('.season-tabs');
    const episodeList = view.querySelector('.episode-list');
    let activeSeasonIdx = 0;

    show.seasons.forEach(function (season, idx) {
      const tab = document.createElement('button');
      tab.className = 'season-tab';
      if (idx === 0) tab.classList.add('active');
      tab.textContent = 'Season ' + season.season;
      tab.addEventListener('click', function () {
        tabsContainer.querySelectorAll('.season-tab').forEach(function (t) {
          t.classList.remove('active');
        });
        tab.classList.add('active');
        activeSeasonIdx = idx;
        renderEpisodes();
      });
      tabsContainer.appendChild(tab);
    });

    function renderEpisodes() {
      episodeList.innerHTML = '';
      const season = show.seasons[activeSeasonIdx];
      if (!season) return;

      season.episodes.forEach(function (ep) {
        const row = episodeRowTpl.content.cloneNode(true);
        row.querySelector('.episode-number').textContent = ep.episode;
        row.querySelector('.episode-name').textContent = episodeDisplayName(ep);

        // Duration from metadata if present
        let durationStr = '';
        if (ep.Metadata && typeof ep.Metadata === 'object' && ep.Metadata.duration_min) {
          var mins = parseInt(ep.Metadata.duration_min, 10);
          if (mins) durationStr = mins + ' min';
        }
        row.querySelector('.episode-duration').textContent = durationStr;

        row.querySelector('.episode-play-btn').addEventListener('click', function (e) {
          e.stopPropagation();
          playEpisode(ep);
        });

        row.querySelector('.episode-row').addEventListener('click', function () {
          playEpisode(ep);
        });

        episodeList.appendChild(row);
      });
    }

    renderEpisodes();
    grid.appendChild(view);
  }

  function playEpisode(ep) {
    try {
      var mediaStore = JSON.parse(localStorage.getItem('media') || '{}');
      mediaStore[ep.ID] = mediaStore[ep.ID] || {};
      mediaStore[ep.ID].name = ep.Name;
      mediaStore[ep.ID].playedFor = mediaStore[ep.ID].playedFor || 0;
      localStorage.setItem('media', JSON.stringify(mediaStore));
    } catch (_) { /* ignore */ }

    window.location.href = '/?play=' + encodeURIComponent(ep.ID);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', fetchShows);
  } else {
    fetchShows();
  }
})();
