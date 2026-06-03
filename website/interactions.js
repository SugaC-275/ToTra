/* ToTra interactive UI behaviors */
document.addEventListener('DOMContentLoaded', () => {

  /* ── 1. SCROLL PROGRESS BAR ── */
  const bar = document.createElement('div');
  bar.id = 'scroll-bar';
  document.body.prepend(bar);
  window.addEventListener('scroll', () => {
    const max = document.documentElement.scrollHeight - window.innerHeight;
    bar.style.width = (max > 0 ? (window.scrollY / max) * 100 : 0) + '%';
  }, { passive: true });

  /* ── 2. ANIMATED NUMBER COUNTERS ── */
  const easeOutCubic = t => 1 - Math.pow(1 - t, 3);
  const statDefs = [
    { suffix: 'ms', prefix: '< ', target: 2, from: 50, decimals: 0 },
    { suffix: '+',  prefix: '',   target: 10, from: 0,  decimals: 0 },
    { suffix: '%',  prefix: '',   target: 100, from: 0, decimals: 0 },
    { suffix: '',   prefix: '',   target: 18,  from: 0, decimals: 0 },
  ];

  function animateCounter(el, def) {
    const dur = 1800;
    const start = performance.now();
    function step(now) {
      const p = Math.min((now - start) / dur, 1);
      const e = easeOutCubic(p);
      const val = Math.round(def.from + (def.target - def.from) * e);
      el.textContent = def.prefix + val + def.suffix;
      if (p < 1) requestAnimationFrame(step);
    }
    requestAnimationFrame(step);
  }

  const statEls = document.querySelectorAll('.stat-n');
  if (statEls.length > 0) {
    const statSection = statEls[0].closest('.stats-bg') || statEls[0].closest('section');
    let fired = false;
    const obs = new IntersectionObserver(entries => {
      if (entries[0].isIntersecting && !fired) {
        fired = true;
        statEls.forEach((el, i) => { if (statDefs[i]) animateCounter(el, statDefs[i]); });
        obs.disconnect();
      }
    }, { threshold: 0.3 });
    if (statSection) obs.observe(statSection);
  }

  /* ── 3. HERO TYPEWRITER ── */
  const heroSub = document.querySelector('.hero-sub');
  const phrases = [
    'Route, optimize, and govern all your LLM traffic in one place.',
    'One line of code. PII blocked. Budgets enforced. Zero refactoring.',
    '8–10× faster than Python-based gateways. Written in Go.',
    'GDPR-ready. Immutable audit chain. EU AI Act compliant.',
  ];

  if (heroSub) {
    heroSub.classList.add('typing');
    let pi = 0, ci = 0, deleting = false;
    let displayTimer = null;

    function typeStep() {
      const phrase = phrases[pi];
      if (!deleting) {
        ci++;
        heroSub.textContent = phrase.slice(0, ci);
        if (ci === phrase.length) {
          displayTimer = setTimeout(() => { deleting = true; typeStep(); }, 2500);
          return;
        }
        setTimeout(typeStep, 45);
      } else {
        ci--;
        heroSub.textContent = phrase.slice(0, ci);
        if (ci === 0) {
          deleting = false;
          pi = (pi + 1) % phrases.length;
          setTimeout(typeStep, 300);
          return;
        }
        setTimeout(typeStep, 25);
      }
    }
    typeStep();
  }

  /* ── 4. CODE TABS ── */
  const codeBox = document.querySelector('.code-box');
  const codeFileSpan = document.querySelector('.code-head .code-file');

  const TABS = {
    Python: { file: 'example.py', html: document.querySelector('.code-box pre')?.innerHTML || '' },
    'Node.js': {
      file: 'example.js',
      html: `<span class="ck">import</span> OpenAI <span class="ck">from</span> <span class="cs">'openai'</span>;
<span class="ck">const</span> <span class="cv">client</span> = <span class="ck">new</span> <span class="cf">OpenAI</span>({
  apiKey: <span class="cs">'your-totra-token'</span>,
  baseURL: <span class="cs">'https://your-totra.railway.app/v1'</span>,
});
<span class="ck">const</span> <span class="cv">resp</span> = <span class="ck">await</span> client.chat.completions.<span class="cf">create</span>({
  model: <span class="cs">'gpt-4o'</span>,
  messages: [{ role: <span class="cs">'user'</span>, content: <span class="cs">'Hello'</span> }],
});`,
    },
    Go: {
      file: 'example.go',
      html: `<span class="cv">client</span> := openai.<span class="cf">NewClient</span>(
  option.<span class="cf">WithAPIKey</span>(<span class="cs">"your-totra-token"</span>),
  option.<span class="cf">WithBaseURL</span>(<span class="cs">"https://your-totra.railway.app/v1"</span>),
)
<span class="cv">resp</span>, _ := client.Chat.Completions.<span class="cf">New</span>(ctx,
  openai.ChatCompletionNewParams{
    Model: openai.ChatModelGPT4o,
    Messages: []openai.ChatCompletionMessageParamUnion{
      openai.<span class="cf">UserMessage</span>(<span class="cs">"Hello"</span>),
    },
  },
)`,
    },
    cURL: {
      file: 'example.sh',
      html: `<span class="cf">curl</span> https://your-totra.railway.app/v1/chat/completions \\
  <span class="ck">-H</span> <span class="cs">"Authorization: Bearer your-totra-token"</span> \\
  <span class="ck">-H</span> <span class="cs">"Content-Type: application/json"</span> \\
  <span class="ck">-d</span> <span class="cs">'{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'</span>`,
    },
  };

  if (codeBox) {
    const pre = codeBox.querySelector('pre');
    const tabBar = document.createElement('div');
    tabBar.className = 'interactions-tabs';

    Object.keys(TABS).forEach((name, i) => {
      const btn = document.createElement('button');
      btn.className = 'tab-btn' + (i === 0 ? ' active' : '');
      btn.textContent = name;
      btn.addEventListener('click', () => {
        tabBar.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        if (pre) pre.innerHTML = TABS[name].html;
        if (codeFileSpan) codeFileSpan.textContent = TABS[name].file;
      });
      tabBar.appendChild(btn);
    });

    codeBox.parentElement.insertBefore(tabBar, codeBox);
  }

  /* ── 5. FEATURE CARD 3D TILT ── */
  document.querySelectorAll('.feat-card').forEach(card => {
    card.addEventListener('mousemove', e => {
      const r = card.getBoundingClientRect();
      const cx = r.left + r.width / 2;
      const cy = r.top + r.height / 2;
      const rx = ((e.clientY - cy) / (r.height / 2)) * -8;
      const ry = ((e.clientX - cx) / (r.width / 2)) * 8;
      card.style.transition = 'transform 0.05s ease';
      card.style.transform = `perspective(600px) rotateX(${rx}deg) rotateY(${ry}deg)`;
    });
    card.addEventListener('mouseleave', () => {
      card.style.transition = 'transform 0.4s ease';
      card.style.transform = 'perspective(600px) rotateX(0deg) rotateY(0deg)';
    });
  });

  /* ── NODE HOVER TOOLTIP ── */
  const tooltip = document.createElement('div');
  tooltip.id = 'node-tooltip';
  tooltip.style.cssText = 'position:fixed;pointer-events:none;background:rgba(6,15,34,0.9);border:1px solid rgba(0,212,255,0.3);color:#00d4ff;font-family:"JetBrains Mono",monospace;font-size:0.7rem;padding:4px 10px;border-radius:4px;opacity:0;transition:opacity 0.15s;z-index:50;';
  document.body.appendChild(tooltip);

  if (window.sceneAPI) {
    window.sceneAPI.onNodeHover = (idx, pos) => {
      if (idx >= 0 && pos) {
        tooltip.textContent = pos.label;
        tooltip.style.left = pos.x + 12 + 'px';
        tooltip.style.top = pos.y - 10 + 'px';
        tooltip.style.opacity = '1';
      } else {
        tooltip.style.opacity = '0';
      }
    };
  } else {
    /* sceneAPI may not be ready yet — defer */
    window.addEventListener('load', () => {
      if (window.sceneAPI) {
        window.sceneAPI.onNodeHover = (idx, pos) => {
          if (idx >= 0 && pos) {
            tooltip.textContent = pos.label;
            tooltip.style.left = pos.x + 12 + 'px';
            tooltip.style.top = pos.y - 10 + 'px';
            tooltip.style.opacity = '1';
          } else {
            tooltip.style.opacity = '0';
          }
        };
      }
    });
  }

});
