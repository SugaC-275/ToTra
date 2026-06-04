/* Three.js hero — ToTra Gateway */
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

  /* GPU-smooth circle particles — no texture, perfect at any resolution */
  function mkPt(hexColor, uSize, opacity) {
    return new THREE.ShaderMaterial({
      uniforms: {
        uColor:   { value: new THREE.Color(hexColor) },
        uSize:    { value: uSize },
        uOpacity: { value: opacity },
      },
      vertexShader: `uniform float uSize;
        void main() {
          vec4 mv = modelViewMatrix * vec4(position, 1.0);
          gl_PointSize = uSize * (900.0 / -mv.z);
          gl_Position = projectionMatrix * mv;
        }`,
      fragmentShader: `uniform vec3 uColor; uniform float uOpacity;
        void main() {
          float d = length(gl_PointCoord - 0.5);
          if (d > 0.5) discard;
          float a = smoothstep(0.5, 0.15, d) * uOpacity;
          gl_FragColor = vec4(uColor, a);
        }`,
      transparent: true, depthWrite: false, blending: ADD,
    });
  }

  /* Lit sphere surface particles — opacity follows world-space normal dot light */
  function mkLitPt(hexColor, uSize, opacity) {
    return new THREE.ShaderMaterial({
      uniforms: {
        uColor:    { value: new THREE.Color(hexColor) },
        uSize:     { value: uSize },
        uOpacity:  { value: opacity },
        uLightDir: { value: new THREE.Vector3(-0.3, 0.8, 1.0).normalize() },
      },
      vertexShader: `uniform float uSize; uniform vec3 uLightDir; varying float vLight;
        void main() {
          vec4 mv = modelViewMatrix * vec4(position, 1.0);
          gl_PointSize = uSize * (900.0 / -mv.z);
          gl_Position = projectionMatrix * mv;
          vec3 wn = normalize((modelMatrix * vec4(normalize(position), 0.0)).xyz);
          vLight = dot(wn, uLightDir);
        }`,
      fragmentShader: `uniform vec3 uColor; uniform float uOpacity; varying float vLight;
        void main() {
          float d = length(gl_PointCoord - 0.5);
          if (d > 0.5) discard;
          float circle = smoothstep(0.5, 0.10, d);
          float light = smoothstep(-0.20, 0.55, vLight);
          float a = circle * uOpacity * light;
          if (a < 0.008) discard;
          gl_FragColor = vec4(uColor, a);
        }`,
      transparent: true, depthWrite: false, blending: ADD,
    });
  }

  /* STARFIELD */
  {
    const pos = new Float32Array(2000 * 3);
    for (let i = 0; i < pos.length; i++) pos[i] = (Math.random() - 0.5) * 140;
    const geo = new THREE.BufferGeometry();
    geo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
    scene.add(new THREE.Points(geo, mkPt(0x2a4a6a, 0.04, 0.55)));
  }

  /* DOT RINGS — wave-animated point orbits */
  const animatables = [];
  function makeDotRing(r, count, hexCol, baseRot, phase, rx, uSize) {
    const cur = new Float32Array(count * 3);
    const geo = new THREE.BufferGeometry();
    const attr = new THREE.BufferAttribute(cur, 3);
    geo.setAttribute('position', attr);
    const m = new THREE.Points(geo, mkPt(hexCol, uSize, 0.80));
    if (rx) m.rotation.x = rx;
    scene.add(m);
    let ang = 0;
    animatables.push({
      update(t, boost) {
        ang += baseRot * boost;
        for (let i = 0; i < count; i++) {
          const a  = (i / count) * Math.PI * 2 + ang;
          const w  = 1 + Math.sin(a * 6 - t * 2.0 + phase) * 0.06;
          cur[i*3]   = Math.cos(a) * r * w;
          cur[i*3+1] = Math.sin(a) * r * w;
          cur[i*3+2] = Math.sin(a * 4 - t * 1.6 + phase) * r * 0.10;
        }
        attr.needsUpdate = true;
      }
    });
  }
  makeDotRing(4.80, 280, 0xaa44ff, 0.00045, 0.00, 0,    0.100);
  makeDotRing(3.80, 200, 0x00aaff, 0.00075, 1.05, 0,    0.120);

  /* CORE — particle sphere + volumetric glow */
  const core = new THREE.Group();
  scene.add(core);
  const R = 2.0;

  /* Dark inner sphere — fills the body visually */
  const innerSphere = new THREE.Mesh(
    new THREE.SphereGeometry(R * 0.96, 28, 20),
    new THREE.MeshBasicMaterial({ color: 0x010b1e, depthWrite: false })
  );
  innerSphere.renderOrder = -1;
  core.add(innerSphere);

  /* Surface particle sphere — SphereGeometry vertices + wave animation */
  {
    const refGeo = new THREE.SphereGeometry(R, 38, 28);
    const base   = refGeo.attributes.position.array.slice();
    const vc     = refGeo.attributes.position.count;
    const cur    = new Float32Array(vc * 3);
    const geo    = new THREE.BufferGeometry();
    const attr   = new THREE.BufferAttribute(cur, 3);
    cur.set(base);
    geo.setAttribute('position', attr);
    core.add(new THREE.Points(geo, mkLitPt(0x88ffff, 0.110, 0.95)));
    refGeo.dispose();
    animatables.push({
      update(t) {
        for (let i = 0; i < vc; i++) {
          const ox = base[i*3], oy = base[i*3+1], oz = base[i*3+2];
          const len = Math.sqrt(ox*ox + oy*oy + oz*oz) || 1;
          const nx = ox/len, ny = oy/len, nz = oz/len;
          const th = Math.atan2(nz, nx);
          const ph = Math.acos(Math.max(-1, Math.min(1, ny)));
          const b  = 1 + Math.sin(ph*5 + th*3 - t*1.4)*0.10
                       + Math.cos(ph*3 - th*2 + t*0.9)*0.07;
          cur[i*3] = nx*R*b; cur[i*3+1] = ny*R*b; cur[i*3+2] = nz*R*b;
        }
        attr.needsUpdate = true;
      }
    });
  }

  /* PROVIDER NODES */
  const LABELS = ['GPT-4o','Claude','Gemini','Llama','Mistral','Cohere'];
  const COLORS  = [0x00d4ff, 0x7c6cfc, 0xf5a623, 0x00e5a0, 0xff6eb4, 0x4a9eff];
  const sphereMeshes = [];
  const nodes = COLORS.map((col, i) => {
    const angle = (i / COLORS.length) * Math.PI * 2, dist = 4.6;
    const g = new THREE.Group();
    g.position.set(Math.cos(angle)*dist, Math.sin(angle)*dist*0.48, (Math.random()-0.5)*1.8);

    /* Single bright circle dot */
    const dg = new THREE.BufferGeometry();
    dg.setAttribute('position', new THREE.BufferAttribute(new Float32Array([0,0,0]), 3));
    g.add(new THREE.Points(dg, mkPt(col, 0.38, 1.0)));

    /* Subtle soft glow behind */
    const glow = new THREE.Mesh(new THREE.SphereGeometry(0.10, 8, 8),
      new THREE.MeshBasicMaterial({ color: col, blending: ADD, transparent: true, opacity: 0.12 }));
    const hit = new THREE.Mesh(new THREE.SphereGeometry(0.28, 8, 8),
      new THREE.MeshBasicMaterial({ visible: false }));
    g.add(glow, hit);
    scene.add(g);
    sphereMeshes.push(hit);
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
      const mesh = new THREE.Mesh(new THREE.SphereGeometry(0.04, 6, 6),
        new THREE.MeshBasicMaterial({ color: n.col, blending: ADD, transparent: true }));
      scene.add(mesh);
      particles.push({ mesh, node: n, t: Math.random(), speed: 0.004+Math.random()*0.003, inbound: false });
    }
  });

  /* BURST POOL */
  const burstPool = Array.from({ length: 25 }, () => {
    const mesh = new THREE.Mesh(new THREE.SphereGeometry(0.05, 6, 6),
      new THREE.MeshBasicMaterial({ color: 0x00d4ff, blending: ADD, transparent: true, opacity: 0 }));
    scene.add(mesh);
    return { mesh, active: false, vel: new THREE.Vector3(), life: 0 };
  });
  function fireBurst(origin, color) {
    burstPool.forEach(b => {
      if (b.active) return;
      b.active = true; b.life = 1;
      b.mesh.material.color.setHex(color);
      b.mesh.position.copy(origin);
      b.vel.set(Math.random()-0.5, Math.random()-0.5, Math.random()-0.5)
        .normalize().multiplyScalar(0.06 + Math.random()*0.06);
    });
  }

  /* GRID FLOOR */
  const grid = new THREE.GridHelper(60, 40, 0x061428, 0x061428);
  grid.position.y = -5; grid.material.transparent = true; grid.material.opacity = 0.4;
  scene.add(grid);

  window.addEventListener('resize', () => {
    W = canvas.offsetWidth; H = canvas.offsetHeight;
    camera.aspect = W / H; camera.updateProjectionMatrix(); renderer.setSize(W, H);
  });

  document.addEventListener('mousemove', e => {
    const rect = canvas.getBoundingClientRect();
    mouse.x = (e.clientX/W - 0.5)*2; mouse.y = -(e.clientY/H - 0.5)*2;
    mouseNDC.x = ((e.clientX - rect.left)/rect.width)*2 - 1;
    mouseNDC.y = -((e.clientY - rect.top)/rect.height)*2 + 1;
    raycaster.setFromCamera(mouseNDC, camera);
    const prev = hoveredNode;
    const hits = raycaster.intersectObjects(sphereMeshes);
    hoveredNode = hits.length ? sphereMeshes.indexOf(hits[0].object) : -1;
    canvas.style.cursor = hoveredNode >= 0 ? 'pointer' : '';
    if (hoveredNode !== prev && window.sceneAPI?.onNodeHover) {
      if (hoveredNode >= 0) {
        const n = nodes[hoveredNode];
        const wp = n.g.getWorldPosition(new THREE.Vector3()).project(camera);
        window.sceneAPI.onNodeHover(hoveredNode, { x:(wp.x*0.5+0.5)*W, y:(-wp.y*0.5+0.5)*H, label:n.label });
      } else { window.sceneAPI.onNodeHover(-1, null); }
    }
  });

  canvas.addEventListener('click', () => {
    if (hoveredNode >= 0) {
      const n = nodes[hoveredNode];
      for (let i = 0; i < 12; i++) {
        const mesh = new THREE.Mesh(new THREE.SphereGeometry(0.055, 6, 6),
          new THREE.MeshBasicMaterial({ color: n.col, blending: ADD, transparent: true, opacity: 0 }));
        scene.add(mesh);
        mesh.position.copy(n.g.position);
        particles.push({ mesh, node: n, t: 0, speed: 0.022+Math.random()*0.012, inbound: true, tempMesh: true });
      }
    } else { fireBurst(new THREE.Vector3(), 0x00d4ff); }
  });

  let scrollProgress = 0;
  window.sceneAPI = {
    triggerBurst() { fireBurst(new THREE.Vector3(), 0x00d4ff); },
    onNodeHover: null,
    get nodes() { return nodes.map((n,i) => ({ index:i, label:n.label, color:n.col })); },
    setScrollProgress(p) { scrollProgress = p; },
  };

  let t = 0;
  (function tick() {
    requestAnimationFrame(tick); t += 0.01;
    const boost = 1 + Math.pow(Math.max(0, scrollProgress - 0.25) / 0.75, 1.5) * 28;
    animatables.forEach(a => a.update(t, boost));
    core.rotation.x += 0.005; core.rotation.z += 0.003;
    core.rotation.y = scrollProgress * Math.PI * 2.8;
    nodes.forEach((n, i) => {
      n.angle += n.speed;
      n.g.position.x = Math.cos(n.angle) * n.dist;
      n.g.position.y = Math.sin(n.angle) * n.dist * 0.48;
      const pos = n.line.geometry.attributes.position;
      pos.setXYZ(1, n.g.position.x, n.g.position.y, n.g.position.z); pos.needsUpdate = true;
      const ts = i === hoveredNode ? 1.4 : 1 + Math.sin(t*2.2+n.angle)*0.12;
      n.g.scale.lerp(new THREE.Vector3(ts, ts, ts), 0.12);
      n.glow.material.opacity = i === hoveredNode ? 0.35 : 0.12;
    });
    for (let i = particles.length - 1; i >= 0; i--) {
      const p = particles[i];
      p.t += p.speed;
      const tp = p.node.g.position;
      const ft = p.inbound ? 1 - p.t : p.t % 1;
      p.mesh.position.set(tp.x*ft, tp.y*ft, tp.z*ft);
      p.mesh.material.opacity = Math.sin((p.inbound ? p.t : p.t%1) * Math.PI) * (p.inbound ? 1 : 0.85);
      if (!p.inbound) p.t = p.t % 1;
      if (p.inbound && p.t >= 1) { scene.remove(p.mesh); particles.splice(i, 1); }
    }
    burstPool.forEach(b => {
      if (!b.active) return;
      b.life -= 0.028; b.mesh.position.add(b.vel); b.vel.multiplyScalar(0.92);
      b.mesh.material.opacity = b.life * 0.9;
      if (b.life <= 0) { b.active = false; b.mesh.material.opacity = 0; }
    });
    camera.position.x += (mouse.x*1.4 - camera.position.x) * 0.035;
    camera.position.y += (0.5 + mouse.y*0.7 - camera.position.y) * 0.035;
    camera.lookAt(0, 0, 0);
    renderer.render(scene, camera);
  })();
})();
