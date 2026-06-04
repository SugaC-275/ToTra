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
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.4;

  const ADD = THREE.AdditiveBlending;
  const mouse = { x: 0, y: 0 };
  const raycaster = new THREE.Raycaster();
  const mouseNDC = new THREE.Vector2();
  let hoveredNode = -1;

  /* LIGHTS — three orbiting colored lights for metallic reflections */
  scene.add(new THREE.AmbientLight(0x0a0a1a, 1.2));
  const orbLights = [
    { lt: new THREE.PointLight(0xfff0cc, 7, 28), r: 8,  y:  3,  angle: 0,    speed:  0.008 },  // warm white — gold highlight
    { lt: new THREE.PointLight(0x88ccff, 4, 22), r: 6,  y: -2,  angle: 2.09, speed: -0.005 },  // cool blue — chrome gleam
    { lt: new THREE.PointLight(0xf5a000, 4, 20), r: 7,  y:  0.5,angle: 4.19, speed:  0.004 },  // amber — deep gold sheen
  ];
  orbLights.forEach(o => scene.add(o.lt));

  /* STARFIELD */
  const sPos = new Float32Array(2000 * 3);
  for (let i = 0; i < sPos.length; i++) sPos[i] = (Math.random() - 0.5) * 140;
  const sGeo = new THREE.BufferGeometry();
  sGeo.setAttribute('position', new THREE.BufferAttribute(sPos, 3));
  scene.add(new THREE.Points(sGeo, new THREE.PointsMaterial({ color: 0x1e3a5a, size: 0.07 })));

  /* EMBLEM — chrome outer + gold inner rings + ⊤ ⊥ T-shapes (正T 反T) */
  const rings = [];

  const mkStd = (col, emit, rough = 0.035) =>
    new THREE.MeshStandardMaterial({ color: col, metalness: 0.97, roughness: rough, emissive: emit, emissiveIntensity: 0.9 });

  const chromeMat = mkStd(0x0c1520, 0x001830);  // cool silver-chrome
  const goldMat   = mkStd(0x1a0c00, 0x0e0600);  // warm dark gold base

  /* helper — torus ring with matching glow halo */
  const addRing = (r, t, mat, speed, glowCol, glowOp = 0.04) => {
    const m = new THREE.Mesh(new THREE.TorusGeometry(r, t, 40, 200), mat);
    m.userData.speed = speed;
    scene.add(m); rings.push(m);
    const h = new THREE.Mesh(new THREE.TorusGeometry(r, t * 5, 18, 120),
      new THREE.MeshBasicMaterial({ color: glowCol, blending: ADD, transparent: true, opacity: glowOp }));
    h.userData.speed = speed;
    scene.add(h); rings.push(h);
  };

  addRing(2.42, 0.062, chromeMat,  0.0007,  0x0055aa, 0.03);  // outer chrome
  addRing(2.06, 0.025, goldMat,   -0.0014,  0x884400, 0.025); // thin gold separator
  addRing(1.72, 0.056, goldMat,    0.0011,  0xaa6600, 0.035); // inner gold

  /* ⊤ upright T  and  ⊥ inverted T — both gold metallic */
  const buildT = (inverted) => {
    const g = new THREE.Group();
    // Horizontal bar — at top for ⊤, at bottom for ⊥
    const bar = new THREE.Mesh(new THREE.BoxGeometry(0.88, 0.105, 0.062), goldMat);
    bar.position.y = inverted ? -0.765 : 0.765;
    // Vertical stem — always centered Y=0, fills between bar and opposite end
    const stem = new THREE.Mesh(new THREE.BoxGeometry(0.105, 1.42, 0.062), goldMat);
    stem.position.y = 0;
    g.add(bar, stem);
    return g;
  };

  const tUp = buildT(false);   // ⊤  left side
  const tDn = buildT(true);    // ⊥  right side
  tUp.position.x = -0.50;
  tDn.position.x =  0.50;
  scene.add(tUp, tDn);

  /* NODES */
  const LABELS = ['GPT-4o','Claude','Gemini','Llama','Mistral','Cohere'];
  const COLORS  = [0x00d4ff,0x7c6cfc,0xf5a623,0x00e5a0,0xff6eb4,0x4a9eff];
  const sphereMeshes = [];
  const nodes = COLORS.map((col, i) => {
    const angle = (i / COLORS.length) * Math.PI * 2, dist = 4.6;
    const g = new THREE.Group();
    g.position.set(Math.cos(angle)*dist, Math.sin(angle)*dist*0.48, (Math.random()-0.5)*1.8);
    const sphere = new THREE.Mesh(new THREE.SphereGeometry(0.13,16,16),
      new THREE.MeshBasicMaterial({ color: col, blending: ADD }));
    const glow = new THREE.Mesh(new THREE.SphereGeometry(0.36,16,16),
      new THREE.MeshBasicMaterial({ color: col, blending: ADD, transparent: true, opacity: 0.14 }));
    g.add(sphere); g.add(glow); scene.add(g);
    sphereMeshes.push(sphere);
    return { g, col, angle, dist, speed: 0.0013 + Math.random()*0.0009, glow, label: LABELS[i] };
  });

  /* CONNECTION LINES */
  nodes.forEach(n => {
    const line = new THREE.Line(
      new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(), n.g.position.clone()]),
      new THREE.LineBasicMaterial({ color: 0x0f2744, transparent: true, opacity: 0.55 }));
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

  /* PUBLIC API */
  window.sceneAPI = {
    triggerBurst() { fireBurst(new THREE.Vector3(), 0x00d4ff); },
    onNodeHover: null,
    get nodes() { return nodes.map((n,i) => ({ index:i, label:n.label, color:n.col })); }
  };

  /* LOOP */
  let t = 0;
  (function tick() {
    requestAnimationFrame(tick); t += 0.01;
    /* orbit lights for metallic sweep */
    orbLights.forEach(o => {
      o.angle += o.speed;
      o.lt.position.set(Math.cos(o.angle)*o.r, o.y, Math.sin(o.angle)*o.r);
    });
    rings.forEach(r => {
      if (r.userData.speed) r.rotation.z += r.userData.speed;
      if (r.userData.follower) r.rotation.copy(r.userData.follower.rotation);
    });
    nodes.forEach((n, i) => {
      n.angle += n.speed;
      n.g.position.x = Math.cos(n.angle)*n.dist;
      n.g.position.y = Math.sin(n.angle)*n.dist*0.48;
      const pos = n.line.geometry.attributes.position;
      pos.setXYZ(1, n.g.position.x, n.g.position.y, n.g.position.z); pos.needsUpdate = true;
      const ts = i === hoveredNode ? 1.4 : 1+Math.sin(t*2.2+n.angle)*0.12;
      n.g.scale.lerp(new THREE.Vector3(ts,ts,ts), 0.12);
      n.glow.material.opacity = i === hoveredNode ? 0.5 : 0.14;
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
