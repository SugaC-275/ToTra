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

  /* ── STARFIELD ── */
  const sPos = new Float32Array(2000 * 3);
  for (let i = 0; i < sPos.length; i++) sPos[i] = (Math.random() - 0.5) * 140;
  const sGeo = new THREE.BufferGeometry();
  sGeo.setAttribute('position', new THREE.BufferAttribute(sPos, 3));
  scene.add(new THREE.Points(sGeo, new THREE.PointsMaterial({ color: 0x1e3a5a, size: 0.07 })));

  /* ── GATEWAY RINGS ── */
  const rings = [];
  [
    { r: 2.4,  t: 0.055, col: 0x00d4ff, rx: 0,            ry: 0,           speed:  0.0025 },
    { r: 2.4,  t: 0.035, col: 0x00d4ff, rx: Math.PI / 2,  ry: 0,           speed: -0.003  },
    { r: 1.25, t: 0.04,  col: 0x7c6cfc, rx: Math.PI / 4,  ry: Math.PI / 5, speed:  0.006  },
    { r: 0.6,  t: 0.028, col: 0xf5a623, rx: -Math.PI / 3, ry: 0,           speed: -0.011  },
  ].forEach(cfg => {
    const geo = new THREE.TorusGeometry(cfg.r, cfg.t, 18, 120);
    const mat = new THREE.MeshBasicMaterial({ color: cfg.col, blending: ADD, transparent: true, opacity: 0.9 });
    const ring = new THREE.Mesh(geo, mat);
    ring.rotation.set(cfg.rx, cfg.ry, 0);
    ring.userData.speed = cfg.speed;
    scene.add(ring);
    rings.push(ring);

    /* glow halo */
    const halo = new THREE.Mesh(
      new THREE.TorusGeometry(cfg.r, cfg.t * 5, 18, 120),
      new THREE.MeshBasicMaterial({ color: cfg.col, blending: ADD, transparent: true, opacity: 0.055 })
    );
    halo.rotation.copy(ring.rotation);
    halo.userData.follower = ring;
    scene.add(halo);
    rings.push(halo);
  });

  /* ── PROVIDER NODES ── */
  const PROVIDER_COLORS = [0x00d4ff, 0x7c6cfc, 0xf5a623, 0x00e5a0, 0xff6eb4, 0x4a9eff];
  const nodes = PROVIDER_COLORS.map((col, i) => {
    const angle = (i / PROVIDER_COLORS.length) * Math.PI * 2;
    const dist = 4.6;
    const g = new THREE.Group();
    g.position.set(
      Math.cos(angle) * dist,
      Math.sin(angle) * dist * 0.48,
      (Math.random() - 0.5) * 1.8
    );

    g.add(new THREE.Mesh(
      new THREE.SphereGeometry(0.13, 16, 16),
      new THREE.MeshBasicMaterial({ color: col, blending: ADD })
    ));
    g.add(new THREE.Mesh(
      new THREE.SphereGeometry(0.36, 16, 16),
      new THREE.MeshBasicMaterial({ color: col, blending: ADD, transparent: true, opacity: 0.14 })
    ));

    scene.add(g);
    return { g, col, angle, dist, baseZ: g.position.z, speed: 0.0013 + Math.random() * 0.0009 };
  });

  /* ── CONNECTION LINES ── */
  nodes.forEach(n => {
    const pts = [new THREE.Vector3(0, 0, 0), n.g.position.clone()];
    const line = new THREE.Line(
      new THREE.BufferGeometry().setFromPoints(pts),
      new THREE.LineBasicMaterial({ color: 0x0f2744, transparent: true, opacity: 0.55 })
    );
    scene.add(line);
    n.line = line;
  });

  /* ── STREAMING PARTICLES ── */
  const particles = [];
  nodes.forEach(n => {
    for (let i = 0; i < 4; i++) {
      const mesh = new THREE.Mesh(
        new THREE.SphereGeometry(0.04, 8, 8),
        new THREE.MeshBasicMaterial({ color: n.col, blending: ADD, transparent: true })
      );
      scene.add(mesh);
      particles.push({ mesh, node: n, t: Math.random(), speed: 0.004 + Math.random() * 0.003 });
    }
  });

  /* ── GRID FLOOR ── */
  const grid = new THREE.GridHelper(60, 40, 0x061428, 0x061428);
  grid.position.y = -5;
  grid.material.transparent = true;
  grid.material.opacity = 0.4;
  scene.add(grid);

  /* ── RESIZE ── */
  window.addEventListener('resize', () => {
    W = canvas.offsetWidth; H = canvas.offsetHeight;
    camera.aspect = W / H;
    camera.updateProjectionMatrix();
    renderer.setSize(W, H);
  });

  document.addEventListener('mousemove', e => {
    mouse.x = (e.clientX / W - 0.5) * 2;
    mouse.y = -(e.clientY / H - 0.5) * 2;
  });

  /* ── LOOP ── */
  let t = 0;
  function tick() {
    requestAnimationFrame(tick);
    t += 0.01;

    /* rotate rings */
    rings.forEach(r => {
      if (r.userData.speed) r.rotation.z += r.userData.speed;
      if (r.userData.follower) r.rotation.copy(r.userData.follower.rotation);
    });

    /* orbit nodes */
    nodes.forEach(n => {
      n.angle += n.speed;
      n.g.position.x = Math.cos(n.angle) * n.dist;
      n.g.position.y = Math.sin(n.angle) * n.dist * 0.48;
      /* update line endpoint */
      const pos = n.line.geometry.attributes.position;
      pos.setXYZ(1, n.g.position.x, n.g.position.y, n.g.position.z);
      pos.needsUpdate = true;
      /* pulse */
      const s = 1 + Math.sin(t * 2.2 + n.angle) * 0.12;
      n.g.scale.setScalar(s);
    });

    /* particles along connections */
    particles.forEach(p => {
      p.t = (p.t + p.speed) % 1;
      const tp = p.node.g.position;
      p.mesh.position.set(tp.x * p.t, tp.y * p.t, tp.z * p.t);
      p.mesh.material.opacity = Math.sin(p.t * Math.PI) * 0.85;
    });

    /* camera parallax */
    camera.position.x += (mouse.x * 1.4 - camera.position.x) * 0.035;
    camera.position.y += (0.5 + mouse.y * 0.7 - camera.position.y) * 0.035;
    camera.lookAt(0, 0, 0);

    renderer.render(scene, camera);
  }
  tick();
})();
