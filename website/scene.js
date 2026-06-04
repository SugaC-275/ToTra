/* Three.js hero scene — gateway portal visualization */
(function () {
  const canvas = document.getElementById('c');
  if (!canvas || typeof THREE === 'undefined') return;

  let W = canvas.offsetWidth, H = canvas.offsetHeight;
  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(52, W / H, 0.1, 200);
  camera.position.set(0, 0.5, 10);

  const renderer = new THREE.WebGLRenderer({ canvas, alpha: true, antialias: true });
  renderer.setSize(W, H);
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  const ADD = THREE.AdditiveBlending;
  const mouse = { x: 0, y: 0 };
  const raycaster = new THREE.Raycaster();
  const mouseNDC = new THREE.Vector2();
  let hoveredNode = -1;

  /* STARFIELD */
  const sPos = new Float32Array(2000 * 3);
  for (let i = 0; i < sPos.length; i++) sPos[i] = (Math.random() - 0.5) * 140;
  const sGeo = new THREE.BufferGeometry();
  sGeo.setAttribute('position', new THREE.BufferAttribute(sPos, 3));
  scene.add(new THREE.Points(sGeo, new THREE.PointsMaterial({ color: 0x1e3a5a, size: 0.07 })));

  /* ── ANIMATED DOT RINGS — no solid torus, pure point-wave ── */
  const animatables = [];

  /* makeDotRing: every dot computes its own (x,y,z) each frame via wave math */
  function makeDotRing(r, count, col, baseRot, phase, rx, size) {
    const cur = new Float32Array(count * 3);
    const geo = new THREE.BufferGeometry();
    const attr = new THREE.BufferAttribute(cur, 3);
    geo.setAttribute('position', attr);
    const m = new THREE.Points(geo, new THREE.PointsMaterial({
      color: col, size, blending: ADD, transparent: true, opacity: 0.72, sizeAttenuation: true
    }));
    if (rx) m.rotation.x = rx;
    scene.add(m);
    let ang = 0;
    animatables.push({
      update(t, boost) {
        ang += baseRot * boost;
        for (let i = 0; i < count; i++) {
          const a  = (i / count) * Math.PI * 2 + ang;
          const w  = 1 + Math.sin(a * 6 - t * 2.0 + phase) * 0.06;
          const rr = r * w;
          cur[i*3]   = Math.cos(a) * rr;
          cur[i*3+1] = Math.sin(a) * rr;
          cur[i*3+2] = Math.sin(a * 4 - t * 1.6 + phase) * r * 0.10;
        }
        attr.needsUpdate = true;
      }
    });
  }

  makeDotRing(4.40, 300, 0x00d4ff, 0.00045, 0.00, 0,    0.048); // outer grand
  makeDotRing(2.72, 210, 0x00d4ff, 0.00075, 1.05, 0,    0.056); // main
  makeDotRing(2.54, 160, 0x44ccff,-0.00060, 2.10, 0,    0.042); // companion
  makeDotRing(1.98, 145, 0x9944ff, 0.00105, 3.14, 0.20, 0.050); // inner violet tilted

  /* ── CORE — high-density Fibonacci sphere with breathing pulse ── */
  const core = new THREE.Group();
  scene.add(core);
  {
    const count = 320, r = 1.02;
    const orig = new Float32Array(count * 3);
    const cur  = new Float32Array(count * 3);
    const phi  = Math.PI * (3 - Math.sqrt(5));
    for (let i = 0; i < count; i++) {
      const y   = 1 - (i / (count - 1)) * 2;
      const rad = Math.sqrt(Math.max(0, 1 - y * y)) * r;
      const th  = phi * i;
      orig[i*3] = cur[i*3] = Math.cos(th) * rad;
      orig[i*3+1] = cur[i*3+1] = y * r;
      orig[i*3+2] = cur[i*3+2] = Math.sin(th) * rad;
    }
    const geo  = new THREE.BufferGeometry();
    const attr = new THREE.BufferAttribute(cur, 3);
    geo.setAttribute('position', attr);
    core.add(new THREE.Points(geo, new THREE.PointsMaterial({
      color: 0x00eeff, size: 0.048, blending: ADD, transparent: true, opacity: 0.80
    })));
    /* dark fill for depth */
    core.add(new THREE.Mesh(new THREE.IcosahedronGeometry(0.90, 1),
      new THREE.MeshBasicMaterial({ color: 0x000b18, transparent: true, opacity: 0.55 })));
    /* per-dot breathing pulse — each dot radiates at its own phase */
    animatables.push({
      update(t) {
        for (let i = 0; i < count; i++) {
          const pulse = 1 + Math.sin(t * 1.8 + i * 0.082) * 0.044;
          cur[i*3]   = orig[i*3]   * pulse;
          cur[i*3+1] = orig[i*3+1] * pulse;
          cur[i*3+2] = orig[i*3+2] * pulse;
        }
        attr.needsUpdate = true;
      }
    });
  }

  /* Core accent — equatorial magenta ring */
  const eqRing = new THREE.Mesh(new THREE.TorusGeometry(0.74, 0.013, 14, 90),
    new THREE.MeshBasicMaterial({ color: 0xff00cc, blending: ADD, transparent: true, opacity: 0.9 }));
  const eqGlow = new THREE.Mesh(new THREE.TorusGeometry(0.74, 0.058, 14, 90),
    new THREE.MeshBasicMaterial({ color: 0xff00cc, blending: ADD, transparent: true, opacity: 0.07 }));
  eqRing.rotation.x = Math.PI / 2; eqGlow.rotation.x = Math.PI / 2;
  core.add(eqRing, eqGlow);
  /* tilted yellow ring */
  const secRing = new THREE.Mesh(new THREE.TorusGeometry(0.70, 0.009, 14, 90),
    new THREE.MeshBasicMaterial({ color: 0xffee00, blending: ADD, transparent: true, opacity: 0.75 }));
  secRing.rotation.set(Math.PI / 4, 0, Math.PI / 6);
  core.add(secRing);
  /* central orb */
  const orb = new THREE.Mesh(new THREE.SphereGeometry(0.16, 16, 16),
    new THREE.MeshBasicMaterial({ color: 0x88ffff, blending: ADD, transparent: true, opacity: 0.9 }));
  const orbGlow = new THREE.Mesh(new THREE.SphereGeometry(0.44, 16, 16),
    new THREE.MeshBasicMaterial({ color: 0x00aaff, blending: ADD, transparent: true, opacity: 0.07 }));
  core.add(orb, orbGlow);

  /* NODES */
  const LABELS = ['GPT-4o','Claude','Gemini','Llama','Mistral','Cohere'];
  const COLORS  = [0x00d4ff,0x7c6cfc,0xf5a623,0x00e5a0,0xff6eb4,0x4a9eff];
  const sphereMeshes = [];
  const nodes = COLORS.map((col, i) => {
    const angle = (i / COLORS.length) * Math.PI * 2, dist = 4.6;
    const g = new THREE.Group();
    g.position.set(Math.cos(angle)*dist, Math.sin(angle)*dist*0.48, (Math.random()-0.5)*1.8);
    const sphere = new THREE.Mesh(new THREE.SphereGeometry(0.055, 12, 12),
      new THREE.MeshBasicMaterial({ color: col, blending: ADD }));
    const glow = new THREE.Mesh(new THREE.SphereGeometry(0.13, 10, 10),
      new THREE.MeshBasicMaterial({ color: col, blending: ADD, transparent: true, opacity: 0.07 }));
    g.add(sphere); g.add(glow); scene.add(g);
    sphereMeshes.push(sphere);
    return { g, col, angle, dist, speed: 0.0013 + Math.random()*0.0009, glow, label: LABELS[i] };
  });

  /* CONNECTION LINES */
  nodes.forEach(n => {
    const line = new THREE.Line(
      new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(), n.g.position.clone()]),
      new THREE.LineBasicMaterial({ color: 0x0a1e33, transparent: true, opacity: 0.18 }));
    scene.add(line); n.line = line;
  });

  /* STREAMING PARTICLES */
  const particles = [];
  nodes.forEach(n => {
    for (let i = 0; i < 4; i++) {
      const mesh = new THREE.Mesh(new THREE.SphereGeometry(0.04,8,8),
        new THREE.MeshBasicMaterial({ color: n.col, blending: ADD, transparent: true }));
      scene.add(mesh);
      particles.push({ mesh, node: n, t: Math.random(), speed: 0.004+Math.random()*0.003, inbound: false });
    }
  });

  /* BURST POOL */
  const burstPool = Array.from({ length: 25 }, () => {
    const mesh = new THREE.Mesh(new THREE.SphereGeometry(0.05,6,6),
      new THREE.MeshBasicMaterial({ color: 0x00d4ff, blending: ADD, transparent: true, opacity: 0 }));
    scene.add(mesh);
    return { mesh, active: false, vel: new THREE.Vector3(), life: 0 };
  });
  function fireBurst(origin, color) {
    burstPool.forEach(b => {
      if (b.active) return;
      b.active = true; b.life = 1;
      b.mesh.material.color.setHex(color || 0x00d4ff);
      b.mesh.position.copy(origin);
      b.vel.set(Math.random()-0.5, Math.random()-0.5, Math.random()-0.5)
        .normalize().multiplyScalar(0.06 + Math.random()*0.06);
    });
  }

  /* GRID FLOOR */
  const grid = new THREE.GridHelper(60,40,0x061428,0x061428);
  grid.position.y = -5; grid.material.transparent = true; grid.material.opacity = 0.4;
  scene.add(grid);

  /* RESIZE */
  window.addEventListener('resize', () => {
    W = canvas.offsetWidth; H = canvas.offsetHeight;
    camera.aspect = W/H; camera.updateProjectionMatrix(); renderer.setSize(W, H);
  });

  /* MOUSE */
  document.addEventListener('mousemove', e => {
    const rect = canvas.getBoundingClientRect();
    mouse.x = (e.clientX/W - 0.5)*2; mouse.y = -(e.clientY/H - 0.5)*2;
    mouseNDC.x = ((e.clientX - rect.left)/rect.width)*2 - 1;
    mouseNDC.y = -((e.clientY - rect.top)/rect.height)*2 + 1;
    raycaster.setFromCamera(mouseNDC, camera);
    const prev = hoveredNode;
    const hits = raycaster.intersectObjects(sphereMeshes);
    hoveredNode = hits.length > 0 ? sphereMeshes.indexOf(hits[0].object) : -1;
    canvas.style.cursor = hoveredNode >= 0 ? 'pointer' : '';
    if (hoveredNode !== prev && window.sceneAPI?.onNodeHover) {
      if (hoveredNode >= 0) {
        const n = nodes[hoveredNode];
        const wp = n.g.getWorldPosition(new THREE.Vector3()).project(camera);
        window.sceneAPI.onNodeHover(hoveredNode,
          { x:(wp.x*0.5+0.5)*W, y:(-wp.y*0.5+0.5)*H, label: n.label });
      } else { window.sceneAPI.onNodeHover(-1, null); }
    }
  });

  canvas.addEventListener('click', () => {
    if (hoveredNode >= 0) {
      const n = nodes[hoveredNode];
      for (let i = 0; i < 18; i++) {
        const mesh = new THREE.Mesh(new THREE.SphereGeometry(0.055,6,6),
          new THREE.MeshBasicMaterial({ color: n.col, blending: ADD, transparent: true, opacity: 0 }));
        scene.add(mesh);
        particles.push({ mesh, node: n, t: 0, speed: 0.022+Math.random()*0.014, inbound: true, tempMesh: true });
      }
    } else { fireBurst(new THREE.Vector3(), 0x00d4ff); }
  });

  let scrollProgress = 0;

  /* PUBLIC API */
  window.sceneAPI = {
    triggerBurst() { fireBurst(new THREE.Vector3(), 0x00d4ff); },
    onNodeHover: null,
    get nodes() { return nodes.map((n,i) => ({ index:i, label:n.label, color:n.col })); },
    setScrollProgress(p) { scrollProgress = p; },
  };

  /* LOOP */
  let t = 0;
  (function tick() {
    requestAnimationFrame(tick); t += 0.01;
    /* dot rings + sphere pulse */
    const spinBoost = 1 + Math.pow(Math.max(0, scrollProgress - 0.25) / 0.75, 1.5) * 28;
    animatables.forEach(a => a.update(t, spinBoost));
    /* core group — self-rotation + scroll Y reveal */
    core.rotation.x += 0.005;
    core.rotation.z += 0.003;
    core.rotation.y = scrollProgress * Math.PI * 2.8;
    /* orb pulse */
    orb.material.opacity     = 0.75 + Math.sin(t * 3.5) * 0.20;
    orbGlow.material.opacity = 0.05 + Math.sin(t * 3.5) * 0.025;
    secRing.rotation.z += 0.009 * spinBoost;
    nodes.forEach((n, i) => {
      n.angle += n.speed;
      n.g.position.x = Math.cos(n.angle)*n.dist;
      n.g.position.y = Math.sin(n.angle)*n.dist*0.48;
      const pos = n.line.geometry.attributes.position;
      pos.setXYZ(1, n.g.position.x, n.g.position.y, n.g.position.z); pos.needsUpdate = true;
      const ts = i === hoveredNode ? 1.4 : 1+Math.sin(t*2.2+n.angle)*0.12;
      n.g.scale.lerp(new THREE.Vector3(ts,ts,ts), 0.12);
      n.glow.material.opacity = i === hoveredNode ? 0.35 : 0.07;
    });
    for (let i = particles.length-1; i >= 0; i--) {
      const p = particles[i];
      p.t += p.speed;
      const tp = p.node.g.position;
      const ft = p.inbound ? 1-p.t : p.t % 1;
      p.mesh.position.set(tp.x*ft, tp.y*ft, tp.z*ft);
      p.mesh.material.opacity = Math.sin((p.inbound ? p.t : p.t%1)*Math.PI) * (p.inbound ? 1 : 0.85);
      if (!p.inbound) p.t = p.t % 1;
      if (p.inbound && p.t >= 1) { scene.remove(p.mesh); particles.splice(i,1); }
    }
    burstPool.forEach(b => {
      if (!b.active) return;
      b.life -= 0.028; b.mesh.position.add(b.vel); b.vel.multiplyScalar(0.92);
      b.mesh.material.opacity = b.life * 0.9;
      if (b.life <= 0) { b.active = false; b.mesh.material.opacity = 0; }
    });
    camera.position.x += (mouse.x*1.4 - camera.position.x)*0.035;
    camera.position.y += (0.5 + mouse.y*0.7 - camera.position.y)*0.035;
    camera.lookAt(0,0,0);
    renderer.render(scene, camera);
  })();
})();
