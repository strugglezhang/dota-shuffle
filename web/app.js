const { createApp } = Vue;

// 壁纸随应用离线提供。首次启动展示远古裂谷，可手动切换。
const WALLPAPERS = [
  'assets/ancient-rift.jpg',
  'assets/dota-battlefield.jpg',
  'assets/dc30aa8fb16c6adb0ac224640b48dd5d21cebd66.jpg'
];

const STAR_OPTIONS = [1, 2, 3, 4, 5].map((value) => ({
  value, label: `${value} 星`, stars: '★'.repeat(value) + '☆'.repeat(5 - value)
}));

const ChoiceMenu = {
  template: '#choice-menu',
  props: ['controlId', 'label', 'placeholder', 'options', 'modelValue', 'disabled'],
  emits: ['update:modelValue'],
  data: () => ({ open: false, active: 0 }),
  computed: {
    selected() {
      return this.options.find((option) => option.value === this.modelValue);
    }
  },
  watch: {
    disabled(value) {
      if (value) this.open = false;
    }
  },
  methods: {
    focus() {
      this.$refs.trigger.focus();
    },
    show(direction = 1) {
      if (this.disabled || !this.options.length) return;
      const selected = this.options.indexOf(this.selected);
      this.active = selected < 0 ? (direction < 0 ? this.options.length - 1 : 0) : selected;
      this.open = true;
      this.$nextTick(() => this.focusOption());
    },
    focusOption() {
      const menu = this.$refs.list;
      if (!menu) return;
      menu.focus({ preventScroll: true });
      const option = menu.children[this.active];
      if (!option) return;
      if (option.offsetTop < menu.scrollTop) menu.scrollTop = option.offsetTop;
      if (option.offsetTop + option.offsetHeight > menu.scrollTop + menu.clientHeight) {
        menu.scrollTop = option.offsetTop + option.offsetHeight - menu.clientHeight;
      }
    },
    close(restoreFocus = false) {
      this.open = false;
      if (restoreFocus) this.focus();
    },
    select(option) {
      this.$emit('update:modelValue', option.value);
      this.close(true);
    },
    onFocusOut(event) {
      if (!this.$el.contains(event.relatedTarget)) this.close();
    },
    onKeyDown(event) {
      if (this.disabled || event.altKey || event.ctrlKey || event.metaKey) return;
      const { key } = event;
      if (!this.open) {
        if (key !== 'ArrowDown' && key !== 'ArrowUp') return;
        event.preventDefault();
        this.show(key === 'ArrowUp' ? -1 : 1);
        return;
      }
      if (key === 'Escape') {
        event.preventDefault();
        this.close(true);
      } else if (key === 'Enter' || key === ' ') {
        event.preventDefault();
        this.select(this.options[this.active]);
      } else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(key)) {
        event.preventDefault();
        const last = this.options.length - 1;
        const next = { ArrowDown: this.active + 1, ArrowUp: this.active - 1, Home: 0, End: last };
        this.active = Math.min(last, Math.max(0, next[key]));
        this.focusOption();
      }
    }
  }
};

const app = createApp({
  data() {
    return {
      // 与 Go 端 rankTable 保持一致的 8 个段位，tier 同时作为下拉值与展示色序号。
      ranks: [
        { tier: 8, name: '冠绝一世' },
        { tier: 7, name: '超凡入圣' },
        { tier: 6, name: '万古流芳' },
        { tier: 5, name: '传奇' },
        { tier: 4, name: '统帅' },
        { tier: 3, name: '中军' },
        { tier: 2, name: '卫士' },
        { tier: 1, name: '先锋' }
      ],
      positions: [
        { value: 1, label: '核心' },
        { value: 2, label: '中单' },
        { value: 3, label: '劣单' },
        { value: 4, label: '辅助' },
        { value: 5, label: '纯辅助' }
      ],
      // positions 为多选可打位置；position 作为主位置随表单一起提交，兼容旧后端与旧数据。
      form: { name: '', rank: '传奇', star: 1, mmr: '', positions: [1] },
      starOptions: STAR_OPTIONS,
      players: [],
      result: null,
      formError: '',
      apiError: '',
      loading: false,
      // history 为 SQLite 中的历史汇总。
      view: 'shuffle',
      history: [],
      viewedRecordId: null,
      viewedOutcome: '',
      series: null,
      swap: null,
      swapStatus: '',
      wallpaperIndex: 0
    };
  },
  mounted() {
    this.resumeSeries();
    this.initWallpaper();
    this.stopParticles = this.initParticles();
  },
  beforeUnmount() {
    this.stopParticles?.();
  },
  computed: {
    // 首发人数：报名超过 10 人时固定取前 10 人，首发必须为偶数才能平分。
    canBalance() {
      return this.players.length >= 2 && this.starterCount % 2 === 0;
    },
    activeSeries() {
      return this.series && !this.series.outcome;
    },
    rosterLocked() {
      return this.loading || this.activeSeries;
    },
    starAvailable() {
      return this.rankTier(this.form.rank) < 8;
    },
    starterCount() {
      return Math.min(this.players.length, 10);
    },
    benchCount() {
      return Math.max(this.players.length - 10, 0);
    },
    starters() {
      return this.players.slice(0, this.starterCount);
    },
    swapOptions() {
      const teams = this.activeSeries ? [this.result.teamA, this.result.teamB] : [];
      return this.starters.map((player) => {
        const team = teams.find((entry) => entry.players.some((starter) => starter.name === player.name));
        const assigned = team && this.assignedPosition(team, player);
        return {
          value: player.name,
          label: player.name,
          badge: team?.name,
          tone: team?.name === '天辉' ? 'radiant' : team ? 'dire' : '',
          detail: team ? (assigned ? `${assigned} 号位 · ${this.posShort(assigned)}` : '待补位') :
            this.playerPositions(player).map((pos) => `${pos} 号位`).join(' / ')
        };
      });
    }
  },
  methods: {
    // 双图层交叉淡入。
    initWallpaper() {
      const slides = document.querySelectorAll('.bg-slide');
      if (!slides.length) return;
      this.bgSlides = slides;
      this.bgActive = 0;
      slides[0].style.backgroundImage = `url("${WALLPAPERS[this.wallpaperIndex]}")`;
      slides[0].classList.add('active');
    },
    rotateWallpaper() {
      if (!this.bgSlides) return;
      const next = (this.wallpaperIndex + 1) % WALLPAPERS.length;
      this.wallpaperIndex = next;
      const target = 1 - this.bgActive;
      this.bgSlides[target].style.backgroundImage = `url("${WALLPAPERS[next]}")`;
      this.bgSlides[target].classList.add('active');
      this.bgSlides[this.bgActive].classList.remove('active');
      this.bgActive = target;
    },

    // ===== 背景漂浮微光粒子：纯 canvas 离线绘制，尊重系统“减少动态效果”设置 =====
    initParticles() {
      const canvas = document.querySelector('.bg-particles');
      if (!canvas || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
      const ctx = canvas.getContext('2d');
      if (!ctx) return;
      let w;
      let h;
      const resize = () => {
        w = canvas.clientWidth;
        h = canvas.clientHeight;
        // Retina/高 DPI 画布按设备像素绘制，并限制超大窗口的绘制开销。
        const ratio = Math.min(window.devicePixelRatio || 1, 2, Math.sqrt(4_000_000 / (w * h)));
        canvas.width = Math.round(w * ratio);
        canvas.height = Math.round(h * ratio);
        ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
      };
      resize();
      window.addEventListener('resize', resize);
      const count = Math.min(70, Math.floor(window.innerWidth / 22));
      const colors = ['89,232,255', '125,240,208', '255,154,91'];
      const particles = Array.from({ length: count }, () => ({
        x: Math.random() * w,
        y: Math.random() * h,
        r: Math.random() * 1.8 + 0.6,
        vx: (Math.random() - 0.5) * 0.35,
        vy: (Math.random() - 0.5) * 0.35,
        color: colors[Math.floor(Math.random() * colors.length)],
        alpha: Math.random() * 0.45 + 0.15
      }));
      let frame;
      const tick = () => {
        ctx.clearRect(0, 0, w, h);
        for (const p of particles) {
          p.x += p.vx;
          p.y += p.vy;
          if (p.x < 0) p.x = w;
          if (p.x > w) p.x = 0;
          if (p.y < 0) p.y = h;
          if (p.y > h) p.y = 0;
          ctx.beginPath();
          ctx.arc(p.x, p.y, p.r, 0, Math.PI * 2);
          ctx.shadowBlur = 8;
          ctx.shadowColor = `rgba(${p.color},0.8)`;
          ctx.fillStyle = `rgba(${p.color},${p.alpha})`;
          ctx.fill();
        }
        frame = requestAnimationFrame(tick);
      };
      frame = requestAnimationFrame(tick);
      return () => {
        cancelAnimationFrame(frame);
        window.removeEventListener('resize', resize);
      };
    },

    posShort(value) {
      return this.positions.find((p) => p.value === value)?.label || `位置${value}`;
    },
    // 兼容旧单位置数据：优先 positions 数组，缺失时回退单 position。
    playerPositions(p) {
      return p.positions && p.positions.length ? p.positions : [p.position];
    },
    // 位置 chip 多选切换，至少保留一个可打位置。
    togglePosition(value) {
      const idx = this.form.positions.indexOf(value);
      if (idx >= 0) {
        if (this.form.positions.length === 1) {
          this.formError = '至少保留一个可打位置。';
          return;
        }
        this.form.positions.splice(idx, 1);
      } else {
        this.form.positions.push(value);
        this.form.positions.sort((a, b) => a - b);
      }
      this.formError = '';
    },
    tierName(tier) {
      const r = this.ranks.find((x) => x.tier === tier);
      return r ? r.name : '-';
    },
    // 段位徽章配色：从低到高由冷灰逐步过渡到金紫，呼应 Dota 段位天梯质感。
    tierColor(tier) {
      const palette = ['#9fb3c8', '#7fd6a6', '#6ac4ff', '#4f9dff', '#a4afff', '#c78bff', '#ffb454', '#ff5a6a'];
      return palette[tier - 1] || '#9fb3c8';
    },
    rankTier(name) {
      return this.ranks.find((r) => r.name === name || String(r.tier) === name)?.tier;
    },
    starLabel(player) {
      return (player.tier || this.rankTier(player.rank)) === 8 ? '—' : `${player.star || 1} 星`;
    },
    addPlayer() {
      if (this.rosterLocked) return;
      this.formError = '';
      const name = this.form.name.trim();
      if (!name) {
        this.formError = '请填写昵称。';
        return;
      }
      if (this.players.length >= 20) {
        this.formError = '报名人数已达上限 20 人（10 人首发 + 最多 10 人替补）。';
        return;
      }
      if (this.players.some((p) => p.name === name)) {
        this.formError = `昵称「${name}」已存在，请换一个。`;
        return;
      }
      if (!this.form.positions.length) {
        this.formError = '请至少选择一个可打位置。';
        return;
      }
      let mmr = 0;
      if (this.form.mmr !== '' && this.form.mmr !== null) {
        mmr = Number(this.form.mmr);
        if (!Number.isSafeInteger(mmr) || mmr < 0) {
          this.formError = 'MMR 分数必须是不小于 0 的整数。';
          return;
        }
      }
      const positions = [...this.form.positions].sort((a, b) => a - b);
      this.players.push({
        name,
        rank: this.form.rank,
        star: this.starAvailable ? this.form.star : 0,
        mmr,
        positions,
        position: positions[0]
      });
      // 保留上次的段位与位置选择，方便连续录入多名队友。
      this.form = {
        name: '',
        rank: this.form.rank,
        star: 1,
        mmr: '',
        positions: [...positions]
      };
    },
    removePlayer(index) {
      if (this.rosterLocked) return;
      this.players.splice(index, 1);
      this.returnToRoster();
    },
    returnToRoster() {
      this.result = null;
      this.series = null;
      this.viewedRecordId = null;
      this.viewedOutcome = '';
      this.apiError = '';
      this.swap = null;
      this.swapStatus = '';
    },
    beginSwap(incoming) {
      if (this.loading || !this.players.slice(this.starterCount).some((player) => player.name === incoming)) return;
      this.swapStatus = '';
      this.swap = { incoming, outgoing: '', error: '' };
      this.$nextTick(() => this.$refs.swapStarter?.focus());
    },
    async confirmSwap() {
      if (this.loading || !this.swap?.outgoing) return;
      const { incoming, outgoing } = this.swap;
      const benchIndex = this.players.findIndex((player) => player.name === incoming);
      const starterIndex = this.starters.findIndex((player) => player.name === outgoing);
      if (benchIndex < this.starterCount || starterIndex < 0) {
        this.swap.error = '首发名单已变化，请重新选择。';
        return;
      }
      if (this.activeSeries) {
        this.loading = true;
        try {
          const response = await fetch(`/api/history/${this.series.id}/substitution`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              incoming, outgoing, rounds: this.series.rounds,
              revision: this.result.substitutions?.length || 0
            })
          });
          const data = await response.json();
          if (!response.ok) throw new Error(data.error || '换人失败。');
          this.applyRecord(data);
          await this.loadHistory();
        } catch (error) {
          if (this.swap) this.swap.error = error.message;
          return;
        } finally {
          this.loading = false;
        }
      } else {
        [this.players[starterIndex], this.players[benchIndex]] =
          [this.players[benchIndex], this.players[starterIndex]];
        this.returnToRoster();
      }
      this.swap = null;
      this.swapStatus = `${incoming} 已升为首发，${outgoing} 已转入替补。`;
    },
    clearAll() {
      if (this.rosterLocked) return;
      this.players = [];
      this.formError = '';
      this.returnToRoster();
    },
    fillSample() {
      if (this.rosterLocked) return;
      const mk = (name, rank, star, mmr, positions) => ({
        name, rank, star, mmr, positions, position: positions[0]
      });
      this.players = [
        mk('BurNIng', '冠绝一世', 0, 6200, [1, 2]),
        mk('Ame', '超凡入圣', 5, 4950, [1]),
        mk('Maybe', '冠绝一世', 0, 5600, [2]),
        mk('SumaiL', '超凡入圣', 4, 4700, [2, 3]),
        mk('Yang', '万古流芳', 5, 4050, [3]),
        mk('Faith_bian', '万古流芳', 2, 3700, [3, 4]),
        mk('Fy', '传奇', 5, 3450, [4]),
        mk('xNova', '传奇', 3, 3200, [4, 5]),
        mk('y', '统帅', 5, 2750, [5]),
        mk('Dy', '统帅', 2, 2400, [5, 1]),
        // 第 11、12 人按录入顺序进入替补席，用于演示首发/替补切分。
        mk('Paparazi', '万古流芳', 4, 4200, [1]),
        mk('Xm', '超凡入圣', 2, 4600, [2])
      ];
      this.returnToRoster();
    },
    async balance() {
      this.apiError = '';
      if (!this.canBalance || this.rosterLocked || this.swap) return;
      this.loading = true;
      try {
        const seed = 1 + Math.floor(Math.random() * 1_000_000_000);
        const resp = await fetch('/api/balance', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ players: this.players, seed, bestOf: 3 })
        });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || '分队失败，请检查输入。');
        this.result = data;
        this.viewedRecordId = data.recordID || null;
        this.viewedOutcome = '';
        this.series = data.recordID ? { id: data.recordID, bestOf: 3, rounds: [], outcome: '' } : null;
        await this.loadHistory();
      } catch (e) {
        this.apiError = e.message;
      } finally {
        this.loading = false;
      }
    },
    async resumeSeries() {
      await this.loadHistory();
      const latest = this.history.find((record) => record.bestOf === 3);
      if (latest && !this.result && this.players.length === 0 && !this.loading && this.view === 'shuffle') {
        await this.loadRecord(latest.id);
      }
    },
    score(series, outcome) {
      return (series.rounds || []).filter((round) => round === outcome).length;
    },
    async recordRound(outcome) {
      if (this.loading || !this.series) return;
      this.loading = true;
      this.apiError = '';
      const id = this.series.id;
      try {
        const resp = await fetch(`/api/history/${id}/rounds`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            previous: this.series.rounds, outcome,
            revision: this.result.substitutions?.length || 0
          })
        });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || '保存战果失败。');
        this.series = { ...data, rounds: data.rounds || [] };
        this.viewedOutcome = data.outcome;
        await this.loadHistory();
      } catch (e) {
        this.apiError = e.message;
      } finally {
        this.loading = false;
      }
    },
    // 拉取历史汇总列表；接口不可用（如纯算法模式）时静默处理，不打扰主流程。
    async loadHistory() {
      try {
        const resp = await fetch('/api/history');
        if (!resp.ok) return;
        const data = await resp.json();
        this.history = data.records || [];
      } catch (e) {
        this.history = [];
      }
    },
    // 回看一条历史：取出当时完整结果并切回分队页渲染，同时带出战果。
    async loadRecord(id) {
      if (this.loading) return;
      this.loading = true;
      this.apiError = '';
      try {
        const resp = await fetch(`/api/history/${id}`);
        const data = await resp.json();
        if (!resp.ok) {
          this.apiError = data.error || '读取历史详情失败。';
          return;
        }
        this.applyRecord(data);
      } catch (e) {
        this.apiError = '无法连接后端服务：' + e.message;
      } finally {
        this.loading = false;
      }
    },
    applyRecord(data) {
      this.result = data.result;
      this.viewedRecordId = data.id;
      this.viewedOutcome = data.outcome || '';
      this.series = data.bestOf === 3 ? { ...data, rounds: data.rounds || [] } : null;
      if (this.series) this.players = data.request.players;
      this.swap = null;
      this.swapStatus = '';
      this.view = 'shuffle';
    },
    // 录入/修改战果：win=天辉胜，lose=天辉负（夜魇胜），''=清除。
    async setOutcome(rec, outcome) {
      this.apiError = '';
      try {
        const resp = await fetch(`/api/history/${rec.id}/outcome`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ outcome })
        });
        const data = await resp.json();
        if (!resp.ok) {
          this.apiError = data.error || '战果录入失败。';
          return;
        }
        rec.outcome = data.outcome;
        if (this.viewedRecordId === rec.id) {
          this.viewedOutcome = data.outcome;
        }
      } catch (e) {
        this.apiError = '无法连接后端服务：' + e.message;
      }
    },
    async removeRecord(id) {
      try {
        const resp = await fetch(`/api/history/${id}`, { method: 'DELETE' });
        if (!resp.ok) {
          const data = await resp.json().catch(() => ({}));
          this.apiError = data.error || '删除历史记录失败。';
          return;
        }
        this.history = this.history.filter((r) => r.id !== id);
        if (this.viewedRecordId === id) this.returnToRoster();
      } catch (e) {
        this.apiError = '无法连接后端服务：' + e.message;
      }
    },
    // 把后端 RFC3339 时间格式化为本地「YYYY-MM-DD HH:mm」。
    formatTime(iso) {
      const d = new Date(iso);
      if (Number.isNaN(d.getTime())) return iso;
      const pad = (n) => String(n).padStart(2, '0');
      return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
    },
    teamScore(team) {
      return team.players.reduce((sum, p) => sum + (p.effMMR ?? 0), 0);
    },
    // 报名总人数 = 两队首发 + 替补。
    rosterTotal(result) {
      return result.teamA.players.length + result.teamB.players.length + (result.bench ? result.bench.length : 0);
    },
    avgTierName(team) {
      const avg = team.players.reduce((s, p) => s + p.tier, 0) / team.players.length;
      return this.tierName(Math.round(avg));
    },
    assignedPosition(team, p) {
      return team.assignments?.[p.name] || 0;
    },
    outcomeLabel(outcome) {
      if (outcome === 'win') return '天辉胜';
      if (outcome === 'lose') return '夜魇胜';
      return '未录入';
    }
  }
});
app.component('choice-menu', ChoiceMenu);
app.mount('#app');
