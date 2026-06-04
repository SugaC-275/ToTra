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

  /* ── NEON RINGS ── */
  const rings = [];
  const neonRing = (r, t, col, speed, glowOp = 0.045) => {
    const m = new THREE.Mesh(new THREE.TorusGeometry(r, t, 24, 160),
      new THREE.MeshBasicMaterial({ color: col, blending: ADD, transparent: true, opacity: 0.85 }));
    m.userData.speed = speed; scene.add(m); rings.push(m);
    const g = new THREE.Mesh(new THREE.TorusGeometry(r, t * 7, 16, 100),
      new THREE.MeshBasicMaterial({ color: col, blending: ADD, transparent: true, opacity: glowOp }));
    g.userData.speed = speed; scene.add(g); rings.push(g);
  };
  neonRing(2.55, 0.010, 0x00d4ff,  0.0006, 0.035); // outer cyan
  neonRing(2.18, 0.007, 0x00d4ff, -0.0010, 0.025); // mid cyan (thin)
  neonRing(1.82, 0.010, 0x8855ff,  0.0008, 0.040); // inner violet

  /* ── CYBERPUNK CORE (scroll-rotates on Y) ── */
  const core = new THREE.Group();
  scene.add(core);

  // Wireframe icosahedron — the gateway lattice
  const icoGeo = new THREE.IcosahedronGeometry(1.0, 1);
  const wireLine = new THREE.LineSegments(
    new THREE.EdgesGeometry(icoGeo),
    new THREE.LineBasicMaterial({ color: 0x00eeff, transparent: true, opacity: 0.75 })
  );
  core.add(wireLine);

  // Dark solid fill for depth
  core.add(new THREE.Mesh(new THREE.IcosahedronGeometry(0.96, 1),
    new THREE.MeshBasicMaterial({ color: 0x000a14, transparent: true, opacity: 0.55 })));

  // Equatorial magenta ring
  const eqRing = new THREE.Mesh(new THREE.TorusGeometry(0.72, 0.013, 14, 90),
    new THREE.MeshBasicMaterial({ color: 0xff00cc, blending: ADD, transparent: true, opacity: 0.9 }));
  const eqGlow = new THREE.Mesh(new THREE.TorusGeometry(0.72, 0.055, 14, 90),
    new THREE.MeshBasicMaterial({ color: 0xff00cc, blending: ADD, transparent: true, opacity: 0.07 }));
  eqRing.rotation.x = Math.PI / 2; eqGlow.rotation.x = Math.PI / 2;
  core.add(eqRing, eqGlow);

  // Tilted secondary ring — electric yellow
  const secRing = new THREE.Mesh(new THREE.TorusGeometry(0.68, 0.009, 14, 90),
    new THREE.MeshBasicMaterial({ color: 0xffee00, blending: ADD, transparent: true, opacity: 0.75 }));
  secRing.rotation.set(Math.PI / 4, 0, Math.PI / 6);
  core.add(secRing);

  // Central energy orb
  const orb = new THREE.Mesh(new THREE.SphereGeometry(0.16, 16, 16),
    new THREE.MeshBasicMaterial({ color: 0x88ffff, blending: ADD, transparent: true, opacity: 0.9 }));
  const orbGlow = new THREE.Mesh(new THREE.SphereGeometry(0.42, 16, 16),
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
    /* outer rings spin (boost on scroll) */
    const spinBoost = 1 + Math.pow(Math.max(0, scrollProgress - 0.25) / 0.75, 1.5) * 28;
    rings.forEach(r => { if (r.userData.speed) r.rotation.z += r.userData.speed * spinBoost; });
    /* core — self-rotation + scroll-driven Y reveal */
    core.rotation.x += 0.005;
    core.rotation.z += 0.003;
    core.rotation.y = scrollProgress * Math.PI * 2.8;  // full revolution on scroll
    /* orb pulse */
    orb.material.opacity = 0.75 + Math.sin(t * 3.5) * 0.2;
    orbGlow.material.opacity = 0.05 + Math.sin(t * 3.5) * 0.025;
    /* secondary ring spin */
    secRing.rotation.z += 0.012 * spinBoost;
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
