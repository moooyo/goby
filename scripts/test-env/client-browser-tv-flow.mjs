/** Browse the original Emby TV UI through visible controls without starting playback. */

const LIBRARIES = ['M3e Reference TV', 'M3e Client Television'];
const SERIES = 'M3e Client Series';
const SEASONS = [
  { number: 1, episodes: ['Episode 1-1', 'Episode 1-2'] },
  { number: 2, episodes: ['Episode 2-1'] },
];
const EPISODE_LABEL = /^(?:S\d+\s*:\s*E\d+\s*-\s*.+|Episode\s*\d+-\d+)$/i;
const ACTIONS = 'button:visible,a:visible,[role="button"]:visible,[role="tab"]:visible,[role="option"]:visible,[role="menuitem"]:visible';
const CONTROLS = `${ACTIONS},select:visible,[role="combobox"]:visible`;

export async function runTVBrowseUI({ page, context, target, report, snapshot, library = 'M3e Reference TV' }) {
  // The caller owns the browser context, target policy, DTO inspection, and logout.
  const result = report.tv_browse = {
    library,
    series: SERIES,
    expected_seasons: SEASONS,
    phase: 'home_library',
    steps: [],
    outcome: 'in_progress',
    method: 'Original client UI navigation and read-only visible DOM observations',
    cleanup_owner: 'caller',
    request_start_index: Array.isArray(report.requests) ? report.requests.length : null,
  };

  function blocked(reason) {
    result.failure_reason = reason;
    const error = new Error('tv_browse_ui_failed');
    error.code = 'TV_BROWSE_UI_FLOW_BLOCKED';
    error.stage = result.phase;
    throw error;
  }

  async function describeControls(locator) {
    return locator.evaluateAll(elements => elements.map((element, index) => {
      const clean = value => String(value ?? '').trim()
        .replace(/(?:https?|wss?|file|blob|data):[^\s"'<>]+/gi, '[redacted URL]')
        .replace(/\b[0-9a-f]{16,}\b/gi, '[redacted ID]')
        .replace(/\b[A-Za-z0-9_-]{40,}\b/g, '[redacted opaque value]')
        .slice(0, 240);
      const ancestors = [];
      for (let parent = element.parentElement, depth = 0; parent && depth < 3; parent = parent.parentElement, depth += 1) {
        ancestors.push({ tag: parent.tagName.toLowerCase(), class: clean(parent.className),
          role: parent.getAttribute('role'), action: clean(parent.getAttribute('data-action')) });
      }
      return {
        index,
        tag: element.tagName.toLowerCase(),
        role: element.getAttribute('role'),
        type: element.getAttribute('type'),
        label: clean(element.getAttribute('aria-label')),
        title: clean(element.getAttribute('title')),
        text: clean(element.innerText),
        class: clean(element.className),
        action: clean(element.getAttribute('data-action')),
        command: clean(element.getAttribute('data-command')),
        has_popup: element.getAttribute('aria-haspopup'),
        expanded: element.getAttribute('aria-expanded'),
        selected: element.getAttribute('aria-selected'),
        current: element.getAttribute('aria-current'),
        disabled: Boolean(element.disabled) || element.getAttribute('aria-disabled') === 'true',
        options: element.tagName === 'SELECT' ? [...element.options].map((option, optionIndex) => ({
          index: optionIndex, text: clean(option.label || option.textContent),
          selected: option.selected, disabled: option.disabled || Boolean(option.closest('optgroup')?.disabled),
        })) : [],
        ancestors,
      };
    }));
  }

  async function inspect(label, { capture = true } = {}) {
    const controls = await describeControls(page.locator(CONTROLS));
    const headings = await page.locator('h1:visible,h2:visible,h3:visible,[role="heading"]:visible')
      .allTextContents();
    const entry = { label, phase: result.phase, observed_at: new Date().toISOString(),
      headings: headings.map(text => text.trim().slice(0, 240)).slice(0, 64),
      controls: controls.slice(0, 220), omitted_control_count: Math.max(0, controls.length - 220) };
    result.steps.push(entry);
    if (capture) {
      try {
        await snapshot(label);
        entry.screenshot = 'captured_by_caller';
      } catch {
        entry.screenshot = 'capture_failed';
        if (result.outcome === 'in_progress') blocked('snapshot_capture_failed');
      }
    }
    return controls;
  }

  function names(control) {
    return [control.label, control.title, control.text].filter(Boolean);
  }

  function safeNavigation(control) {
    const action = /^(?:play|resume|shuffle|queue|instantmix|favorite|delete|edit|mark|rating|toggle(?:play|favorite))/i;
    return !control.disabled && control.type !== 'submit' &&
      ![control.action, control.command, ...control.ancestors.map(parent => parent.action)].some(value => action.test(value)) &&
      !names(control).some(name => /^(?:Play|Resume|Shuffle|Add to Queue|Mark as Played|Mark as Unplayed)(?:\s|$)/i.test(name));
  }

  function seasonNumber(text) {
    const match = /^(?:[\ue5ca\ue2c7]\s*)?Season\s+0*([12])(?:\s*\(\d+(?:\s+episodes?)?\))?$/i.exec(text.trim());
    return match ? Number(match[1]) : null;
  }

  function isSeasonDropdown(control) {
    return control.tag !== 'select' && !['option', 'menuitem', 'tab'].includes(control.role) &&
      (control.role === 'combobox' || ['true', 'listbox', 'menu'].includes(control.has_popup) ||
        /select|dropdown/i.test(control.class)) && names(control).some(name =>
        seasonNumber(name) || /^(?:Season|Seasons|All Episodes)$/i.test(name));
  }

  function isObservedLazySeasonSelect(control) {
    return control.tag === 'select' && control.options.every(option => !option.text) &&
      /(?:^|\s)detailSelectSeason(?:\s|$)/.test(control.class) &&
      control.ancestors.some(parent => parent.tag === 'label' && /(?:^|\s)selectLabel-inline(?:\s|$)/.test(parent.class)) &&
      control.ancestors.some(parent => /(?:^|\s)detailSelectSeasonContainer(?:\s|$)/.test(parent.class));
  }

  function isSeasonPopupOption(control) {
    return ['option', 'menuitem'].includes(control.role) || /(?:^|\s)actionSheetMenuItem(?:\s|$)/.test(control.class);
  }

  async function clickObserved(locator, control, action) {
    if (!safeNavigation(control)) blocked('selected_control_is_not_safe_browse_navigation');
    result.steps.push({ label: `${result.phase}-action`, phase: result.phase, action, control });
    await locator.click({ timeout: 8000 });
  }

  async function titleControls(title) {
    const text = page.getByText(title, { exact: true }).filter({ visible: true });
    const controls = text.locator('xpath=ancestor-or-self::*[self::button or self::a][1]').filter({ visible: true });
    const descriptions = await describeControls(controls);
    return { locator: controls, descriptions, candidates: descriptions.filter(safeNavigation) };
  }

  function exactCardTitles(found, title) {
    return found.candidates.filter(control => control.action === 'link' &&
      names(control).includes(title) && control.ancestors.some(parent => /(?:^|\s)card(?:Box)?(?:\s|$)/i.test(parent.class)));
  }

  async function clickTitle(title, action, { requireCard = false } = {}) {
    const found = await titleControls(title);
    const cardTitles = exactCardTitles(found, title);
    if (requireCard && cardTitles.length !== 1) blocked(`exact_card_title_control_${cardTitles.length ? 'ambiguous' : 'missing'}`);
    let control;
    if (cardTitles.length === 1) {
      control = cardTitles[0];
      result.steps.push({ label: `${result.phase}-title-resolution`, phase: result.phase,
        title, resolution: 'unique_exact_link_title_in_visible_card',
        visible_title_candidate_count: found.candidates.length, control });
    } else {
      if (found.candidates.length !== 1) blocked(`exact_title_control_${found.candidates.length ? 'ambiguous' : 'missing'}`);
      control = found.candidates[0];
    }
    await clickObserved(found.locator.nth(control.index), control, action);
  }

  async function waitForTitle(title, timeout = 15000) {
    await page.getByText(title, { exact: true }).filter({ visible: true }).first()
      .waitFor({ state: 'visible', timeout });
  }

  async function waitForDOM(predicate, reason, timeout = 15000) {
    const deadline = Date.now() + timeout;
    do {
      if (await predicate()) return;
      await page.waitForTimeout(250);
    } while (Date.now() < deadline);
    blocked(reason);
  }

  async function exactAction(expression, action, { optional = false, tabOnly = false } = {}) {
    const locator = page.locator(ACTIONS);
    const controls = await describeControls(locator);
    const candidates = controls.filter(control => safeNavigation(control) &&
      (!tabOnly || control.role === 'tab' || /(?:^|\s)(?:emby-tab-button|tabButton)(?:\s|$)/i.test(control.class)) &&
      names(control).some(name => expression.test(name)));
    if (!candidates.length && optional) return false;
    if (candidates.length !== 1) blocked(`navigation_control_${candidates.length ? 'ambiguous' : 'missing'}`);
    await clickObserved(locator.nth(candidates[0].index), candidates[0], action);
    return true;
  }

  async function readEpisodeTitles() {
    // Include arbitrary prefixed names so an incorrect title cannot disappear from the comparison.
    return page.getByText(EPISODE_LABEL).filter({ visible: true }).evaluateAll(elements => elements.map(element => {
      const fullLabel = element.innerText.trim();
      const prefix = /^S(\d+)\s*:\s*E(\d+)\s*-\s*(.+)$/i.exec(fullLabel);
      const name = prefix?.[3] ?? fullLabel;
      const match = /^Episode\s*(\d+)-(\d+)$/i.exec(name);
      const nameSeason = match ? Number(match[1]) : null;
      const nameEpisode = match ? Number(match[2]) : null;
      const season = prefix ? Number(prefix[1]) : nameSeason;
      const episode = prefix ? Number(prefix[2]) : nameEpisode;
      return { full_label: fullLabel, name,
        title: match ? `Episode ${nameSeason}-${nameEpisode}` : null, season, episode,
        has_season_episode_prefix: Boolean(prefix),
        prefix_metadata_matches_name: Boolean(prefix && match) && season === nameSeason && episode === nameEpisode,
        card_context: Boolean(element.closest('.card,.cardBox,.listItem,[role="row"],[role="listitem"]')),
        title_navigation_control: Boolean(element.closest('button,a')) };
    }));
  }

  async function waitForSeasonUI(timeout = 15000) {
    const deadline = Date.now() + timeout;
    do {
      const controls = await describeControls(page.locator(CONTROLS));
      if (controls.some(control => isObservedLazySeasonSelect(control) || control.options.some(option => seasonNumber(option.text)) ||
          names(control).some(name => seasonNumber(name) || /^(?:Season|Seasons|All Episodes)$/i.test(name)))) return;
      await page.waitForTimeout(250);
    } while (Date.now() < deadline);
    blocked('series_season_navigation_not_observed');
  }

  async function returnToSeries() {
    const title = await titleControls(SERIES);
    if (title.candidates.length === 1) {
      const control = title.candidates[0];
      await clickObserved(title.locator.nth(control.index), control, 'click_exact_series_breadcrumb');
    } else {
      await exactAction(/^Back$/i, 'click_visible_back_control');
    }
    await waitForSeasonUI();
    await inspect('tv-series-returned-for-next-season');
  }

  async function selectSeason(number, { allowSeriesReturn = false } = {}) {
    let seasonsTabOpened = false;
    let episodesTabOpened = false;
    let dropdownOpened = false;
    let returnedToSeries = false;
    for (let attempt = 0; attempt < 5; attempt += 1) {
      const locator = page.locator(CONTROLS);
      const controls = await describeControls(locator);
      const selectors = controls.filter(control => !dropdownOpened && safeNavigation(control) && control.tag === 'select' &&
        SEASONS.every(season => control.options.some(option => !option.disabled && seasonNumber(option.text) === season.number)));
      if (selectors.length > 1) blocked('season_select_control_ambiguous');
      if (selectors.length === 1) {
        const selector = selectors[0];
        const options = selector.options.filter(option => !option.disabled && seasonNumber(option.text) === number);
        if (options.length !== 1) blocked('season_select_option_ambiguous');
        result.steps.push({ label: `${result.phase}-action`, phase: result.phase,
          action: 'select_observed_season_option', season: number, control: selector, option: options[0] });
        await locator.nth(selector.index).selectOption({ index: options[0].index }, { timeout: 8000 });
        return 'native_season_selector';
      }

      const lazySelectors = controls.filter(control => safeNavigation(control) && isObservedLazySeasonSelect(control));
      if (!dropdownOpened && lazySelectors.length > 1) blocked('empty_season_select_control_ambiguous');
      if (!dropdownOpened && lazySelectors.length === 1) {
        const select = lazySelectors[0];
        const label = locator.nth(select.index).locator('xpath=ancestor::label[1]').filter({ visible: true });
        const labels = await describeControls(label);
        if (labels.length !== 1) blocked('empty_season_select_label_missing_or_ambiguous');
        result.steps.push({ label: `tv-season-${number}-lazy-select`, phase: result.phase,
          resolution: 'observed_empty_detail_season_select_with_visible_ancestor_label', control: select });
        await clickObserved(label, labels[0], 'open_observed_empty_season_select_label');
        dropdownOpened = true;
        await waitForDOM(async () => (await describeControls(page.locator(CONTROLS))).some(control =>
          safeNavigation(control) && isSeasonPopupOption(control) && names(control).some(name => seasonNumber(name) === number)),
        'requested_season_option_not_observed_after_label_click');
        await inspect(`tv-season-${number}-lazy-select-options`);
        continue;
      }

      const seasonControls = controls.filter(control => safeNavigation(control) && control.tag !== 'select' && !isSeasonDropdown(control) &&
        names(control).some(name => seasonNumber(name)));
      const candidates = seasonControls.filter(control => names(control).some(name => seasonNumber(name) === number));
      const bothSeasonsVisible = SEASONS.every(season => seasonControls.some(control =>
        names(control).some(name => seasonNumber(name) === season.number)));
      const selected = candidates.filter(control => dropdownOpened ? isSeasonPopupOption(control) :
        bothSeasonsVisible || control.role === 'option' || control.role === 'menuitem' || control.role === 'tab');
      if (selected.length > 1) blocked('season_title_control_ambiguous');
      if (selected.length === 1) {
        if (dropdownOpened) result.steps.push({ label: `tv-season-${number}-popup-resolution`, phase: result.phase,
          resolution: 'visible_season_popup_items_only', popup_candidate_count: selected.length,
          excluded_background_candidate_count: candidates.length - selected.length });
        await clickObserved(locator.nth(selected[0].index), selected[0], 'click_observed_season_control');
        return dropdownOpened ? 'visible_season_menu' : 'visible_season_navigation';
      }

      const dropdowns = controls.filter(control => safeNavigation(control) && isSeasonDropdown(control));
      if (!dropdownOpened && dropdowns.length > 1) blocked('season_dropdown_control_ambiguous');
      if (!dropdownOpened && dropdowns.length === 1) {
        await clickObserved(locator.nth(dropdowns[0].index), dropdowns[0], 'open_observed_season_dropdown');
        dropdownOpened = true;
        await waitForDOM(async () => (await describeControls(page.locator(CONTROLS))).some(control =>
          safeNavigation(control) && isSeasonPopupOption(control) &&
          names(control).some(name => seasonNumber(name) === number)), 'requested_season_option_not_observed');
        await inspect(`tv-season-${number}-dropdown`);
        continue;
      }
      if (dropdownOpened) blocked('requested_season_option_not_observed');

      if (!seasonsTabOpened && await exactAction(/^Seasons$/i, 'open_visible_seasons_navigation', { optional: true })) {
        seasonsTabOpened = true;
        await waitForDOM(async () => (await describeControls(page.locator(CONTROLS))).some(control =>
          control.options.some(option => seasonNumber(option.text) === number) ||
          isSeasonDropdown(control) || names(control).some(name => seasonNumber(name) === number)),
        'season_controls_not_observed_after_seasons_navigation');
        await inspect(`tv-season-${number}-seasons-view`);
        continue;
      }
      if (!episodesTabOpened && await exactAction(/^Episodes$/i, 'open_visible_episodes_navigation', { optional: true })) {
        episodesTabOpened = true;
        await waitForDOM(async () => (await describeControls(page.locator(CONTROLS))).some(control =>
          control.options.some(option => seasonNumber(option.text) === number) || isSeasonDropdown(control)),
        'season_controls_not_observed_after_episodes_navigation');
        await inspect(`tv-season-${number}-episodes-view`);
        continue;
      }
      if (allowSeriesReturn && !returnedToSeries) {
        await returnToSeries();
        returnedToSeries = true;
        seasonsTabOpened = false;
        episodesTabOpened = false;
        continue;
      }
      blocked('requested_season_navigation_not_observed');
    }
    blocked('season_navigation_transition_limit_reached');
  }

  async function confirmSeason(season) {
    const expected = new Set(season.episodes);
    const ownedEpisodes = SEASONS.flatMap(ownedSeason => ownedSeason.episodes.map((title, index) =>
      ({ title, season: ownedSeason.number, episode: index + 1 })));
    const deadline = Date.now() + 15000;
    let observed = [];
    let selectionLabels = [];
    do {
      observed = await readEpisodeTitles();
      selectionLabels = (await describeControls(page.locator('.detailSelectSeasonContainer:visible label.selectLabel-inline:visible')))
        .map(control => {
          const text = control.text.replace(/^\ue313\s*/u, '');
          const match = /^Season\s+0*([12])$/i.exec(text);
          return { control, full_label: control.text, parsed_season: match ? Number(match[1]) : null };
        });
      const selectionMatches = selectionLabels.length === 1 && selectionLabels[0].parsed_season === season.number;
      const validOwnedEntries = observed.every(entry => entry.has_season_episode_prefix && entry.prefix_metadata_matches_name &&
        entry.card_context && entry.title_navigation_control && ownedEpisodes.some(owned =>
          entry.title === owned.title && entry.name === owned.title && entry.season === owned.season && entry.episode === owned.episode));
      const uniqueEntries = new Set(observed.map(entry => `${entry.season}:${entry.episode}:${entry.title}`)).size === observed.length;
      const selectedEpisodes = observed.filter(entry => entry.season === season.number);
      const selectedSeasonMatches = selectedEpisodes.length === expected.size &&
        selectedEpisodes.every(entry => expected.has(entry.title));
      const filtered = observed.length === expected.size && observed.every(entry => entry.season === season.number);
      const crossSeason = observed.length === ownedEpisodes.length && ownedEpisodes.every(owned =>
        observed.some(entry => entry.title === owned.title && entry.season === owned.season && entry.episode === owned.episode));
      if (selectionMatches && validOwnedEntries && uniqueEntries && selectedSeasonMatches && (filtered || crossSeason)) {
        const controls = await describeControls(page.locator(CONTROLS));
        const selection = controls.filter(control => control.options.some(option => option.selected && seasonNumber(option.text)) ||
          (control.selected === 'true' || control.current === 'page') && names(control).some(name => seasonNumber(name)));
        const entry = { label: `tv-season-${season.number}-episodes`, phase: result.phase,
          season: season.number, expected_titles: season.episodes, observed_titles: observed,
          all_observed_labels: observed.map(episode => episode.full_label), selected_season_episode_titles: selectedEpisodes,
          selected_season_label: selectionLabels[0], selected_season_controls: selection,
          selection_label_matches_requested_season: true,
          view_mode: crossSeason ? 'cross_season_series_list' : 'selected_season_list',
          complete_owned_episode_set_visible: crossSeason,
          other_season_titles_absent: filtered, strict_season_filtering_observed: filtered };
        result.steps.push(entry);
        (result.season_evidence ??= []).push(entry);
        result.view_mode = result.season_evidence.some(evidence => evidence.view_mode === 'cross_season_series_list')
          ? 'cross_season_series_list' : 'selected_season_list';
        result.strict_season_filtering_observed = result.season_evidence.every(evidence => evidence.strict_season_filtering_observed);
        result.other_season_titles_absent = result.season_evidence.every(evidence => evidence.other_season_titles_absent);
        await inspect(`tv-season-${season.number}-episodes-visible`);
        return;
      }
      await page.waitForTimeout(250);
    } while (Date.now() < deadline);
    result.steps.push({ label: `tv-season-${season.number}-episode-mismatch`, phase: result.phase,
      season: season.number, expected_titles: season.episodes, observed_titles: observed,
      all_observed_labels: observed.map(episode => episode.full_label), selected_season_labels: selectionLabels });
    blocked('selected_season_label_or_visible_owned_episode_set_invalid');
  }

  async function confirmEpisodeDetail(previousURL) {
    const title = 'Episode 2-1';
    const deadline = Date.now() + 15000;
    let evidence = null;
    do {
      const titleElements = await page.getByRole('heading', { name: EPISODE_LABEL }).filter({ visible: true }).evaluateAll(elements => {
        const cards = '.card,.cardBox,.listItem,[role="row"],[role="listitem"]';
        return elements.map(element => {
          const rawLabel = element.innerText.trim();
          const prefix = /^S(\d+)\s*:\s*E(\d+)\s*-\s*(.+)$/i.exec(rawLabel);
          const name = prefix?.[3] ?? rawLabel;
          const episodeName = /^Episode\s*(\d+)-(\d+)$/i.exec(name);
          const scope = element.closest('.detailTextContainer,.detailMainContainer,.detailPageContent,.itemDetailPage') || element.closest('main,[role="main"]');
          const metadata = scope ? [scope, ...scope.querySelectorAll('*')].filter(node => {
            const rect = node.getBoundingClientRect();
            return rect.width > 0 && rect.height > 0 && getComputedStyle(node).visibility !== 'hidden' &&
              !node.closest(cards) && !node.querySelector(cards);
          }).flatMap(node => (node.innerText ?? '').split(/[\r\n]+/)).map(line => line.trim()).filter(line =>
            line && line.length <= 240 && /\b(?:Season\s*0?2|S0?2\s*[,.:/-]?\s*E0?1|Episode\s*0?1|E0?1)\b/i.test(line)) : [];
          return { tag: element.tagName.toLowerCase(), role: element.getAttribute('role'),
            class: typeof element.className === 'string' ? element.className.slice(0, 240) : '',
            raw_label: rawLabel,
            parsed_identity: { name, season: prefix ? Number(prefix[1]) : null, episode: prefix ? Number(prefix[2]) : null,
              name_season: episodeName ? Number(episodeName[1]) : null, name_episode: episodeName ? Number(episodeName[2]) : null },
            detail_context: Boolean(scope), card_context: Boolean(element.closest(cards)),
            content_scope: scope ? { tag: scope.tagName.toLowerCase(),
              class: typeof scope.className === 'string' ? scope.className.slice(0, 240) : '' } : null,
            metadata_lines: [...new Set(metadata)].slice(0, 32) };
        });
      });
      const detailTitles = titleElements.filter(element => element.detail_context && !element.card_context);
      const detailHeading = detailTitles.length === 1 ? detailTitles[0] : null;
      const identity = detailHeading?.parsed_identity ?? null;
      const headingNameMatches = Boolean(identity) && identity.name === title && identity.name_season === 2 && identity.name_episode === 1;
      const headingPrefixMatches = headingNameMatches && identity.season === 2 && identity.episode === 1;
      const metadataLines = detailTitles.length === 1 ? detailTitles[0].metadata_lines : [];
      const seasonVisible = metadataLines.some(line => /\bSeason\s*0?2\b(?![-\d])/i.test(line));
      const episodeVisible = metadataLines.some(line => /\b(?:Episode\s*|E)0?1\b(?![-\d])/i.test(line));
      const compactVisible = metadataLines.some(line => /\bS0?2\s*[,.:/-]?\s*E0?1\b(?![-\d])/i.test(line));
      evidence = { title, season: 2, episode: 1, detail_title_observed: headingPrefixMatches,
        unique_detail_heading_observed: detailTitles.length === 1, detail_heading_raw: detailHeading?.raw_label ?? null,
        parsed_heading_identity: identity, detail_heading_prefix_matches_fixture: headingPrefixMatches,
        page_navigation_observed: page.url() !== previousURL, title_elements: titleElements,
        metadata_lines: metadataLines,
        metadata_source: headingPrefixMatches ? 'unique_visible_non_card_detail_heading' : 'same_detail_content_metadata',
        season_and_episode_metadata_observed: headingPrefixMatches || compactVisible || (seasonVisible && episodeVisible) };
      if (evidence.page_navigation_observed && evidence.detail_title_observed && evidence.season_and_episode_metadata_observed) {
        result.episode_detail = evidence;
        await inspect('tv-episode-detail');
        return;
      }
      await page.waitForTimeout(250);
    } while (Date.now() < deadline);
    result.episode_detail = evidence;
    blocked('episode_detail_title_and_season_episode_metadata_not_observed');
  }

  async function returnHome() {
    result.phase = 'return_home';
    const home = page.getByRole('link', { name: 'Home', exact: true }).filter({ visible: true });
    const descriptions = await describeControls(home);
    if (descriptions.length !== 1) blocked('exact_home_link_missing_or_ambiguous');
    const detailURL = page.url();
    const detailHeading = result.episode_detail?.detail_heading_raw ?? 'Episode 2-1';
    await clickObserved(home, descriptions[0], 'click_exact_home_link');
    await waitForDOM(async () => {
      if (page.url() === detailURL) return false;
      const libraryTitle = await titleControls(library);
      return libraryTitle.candidates.some(control => control.ancestors.some(parent => /card|itemsContainer/i.test(parent.class))) &&
        await page.getByRole('heading', { name: detailHeading, exact: true }).filter({ visible: true }).count() === 0;
    }, 'home_library_card_and_departure_from_episode_detail_not_observed');
    result.return_home = 'home_link_clicked_navigation_observed_and_home_tv_library_card_visible';
    await inspect('tv-returned-home');
  }

  try {
    if (!LIBRARIES.includes(library)) blocked('unsupported_tv_library_title');
    await waitForTitle(library, 20000);
    await inspect('tv-home-before');
    const homeURL = page.url();
    await clickTitle(library, 'open_exact_tv_library_title');

    result.phase = 'library_series';
    let libraryNavigation = [];
    await waitForDOM(async () => {
      if (page.url() === homeURL) return false;
      libraryNavigation = (await describeControls(page.locator(ACTIONS))).filter(control => safeNavigation(control) &&
        names(control).some(name => /^(?:Shows|Series)$/i.test(name)));
      return libraryNavigation.length > 0;
    }, 'tv_library_navigation_and_series_tab_not_observed');
    result.steps.push({ label: 'tv-library-navigation-ready', phase: result.phase,
      page_navigation_observed: true, controls: libraryNavigation });
    const series = await titleControls(SERIES);
    if (exactCardTitles(series, SERIES).length !== 1) {
      await inspect('tv-library-before-series-navigation');
      await exactAction(/^(?:Shows|Series)$/i, 'open_visible_series_navigation', { optional: true });
    }
    await waitForDOM(async () => exactCardTitles(await titleControls(SERIES), SERIES).length === 1,
      'unique_series_card_title_not_observed');
    await inspect('tv-library-series');
    // The snapshot can span a transition, so reacquire the visible card immediately before clicking.
    await waitForDOM(async () => exactCardTitles(await titleControls(SERIES), SERIES).length === 1,
      'unique_series_card_title_not_observed_after_snapshot');
    const libraryURL = page.url();
    await clickTitle(SERIES, 'open_exact_series_title', { requireCard: true });

    result.phase = 'series_seasons';
    await waitForDOM(async () => page.url() !== libraryURL &&
      await page.getByRole('heading', { name: SERIES, exact: true }).filter({ visible: true }).count() === 1,
    'series_navigation_and_detail_heading_not_observed');
    result.steps.push({ label: 'tv-series-detail-navigation-ready', phase: result.phase,
      page_navigation_observed: true, detail_heading: SERIES });
    await waitForSeasonUI();
    await inspect('tv-series-seasons');
    for (const season of SEASONS) {
      result.phase = `season_${season.number}`;
      const navigation = await selectSeason(season.number, { allowSeriesReturn: season.number === 2 });
      result.steps.push({ label: `tv-season-${season.number}-selection`, phase: result.phase,
        season: season.number, navigation });
      await confirmSeason(season);
    }

    result.phase = 'episode_detail';
    const episodeTitles = (await readEpisodeTitles()).filter(entry => entry.title === 'Episode 2-1' &&
      entry.season === 2 && entry.episode === 1 && entry.prefix_metadata_matches_name && entry.title_navigation_control);
    if (episodeTitles.length !== 1) blocked('selected_episode_title_missing_or_ambiguous');
    const seasonURL = page.url();
    await clickTitle(episodeTitles[0].full_label, 'open_exact_episode_title_detail');
    await confirmEpisodeDetail(seasonURL);
    await returnHome();
    result.phase = 'complete';
    result.outcome = result.view_mode === 'cross_season_series_list'
      ? 'tv_browse_ui_flow_completed_with_cross_season_list' : 'tv_browse_ui_flow_completed';
    return result;
  } catch {
    result.outcome = 'blocked_at_observed_ui_step';
    result.failure_reason ??= 'ui_action_or_observation_failed';
    await inspect(`tv-blocked-${result.phase}`).catch(() => {});
    const seasonMenu = page.locator('button.actionSheetMenuItem:visible').filter({ hasText: /Season\s+0*[12]\b/ });
    if (await seasonMenu.count()) {
      await page.keyboard.press('Escape').catch(() => {});
      await seasonMenu.first().waitFor({ state: 'hidden', timeout: 3000 }).catch(() => {});
      result.failed_menu_cleanup = await seasonMenu.count() ? 'ui_escape_did_not_dismiss_menu' : 'ui_escape_dismissed_season_menu';
    }
    const error = new Error('tv_browse_ui_failed');
    error.code = 'TV_BROWSE_UI_FLOW_BLOCKED';
    error.stage = result.phase;
    throw error;
  } finally {
    if (Array.isArray(report.requests) && result.request_start_index !== null) {
      result.request_end_index = report.requests.length;
      result.observed_browse_gets = report.requests.slice(result.request_start_index).filter(entry =>
        entry.method === 'GET' && entry.origin === 'target' &&
        /^(?:\/emby)?\/(?:Shows\/[^/]+\/(?:Seasons|Episodes)|Users\/[^/]+\/Items(?:\/[^/]+)?|Items(?:\/[^/]+)?)\/?$/i.test(entry.route))
        .map(entry => ({ method: entry.method, route: entry.route, origin: entry.origin, status: entry.status }));
    }
  }
}
