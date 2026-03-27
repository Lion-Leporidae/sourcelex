// Sourcelex Explorer - 代码图谱浏览器
(function () {
  "use strict";

  // ===== State =====
  const state = {
    graphData: null,
    simulation: null,
    selectedNode: null,
    zoomBehavior: null,
    gElement: null,
  };

  const colorMap = { function: "#58a6ff", class: "#3fb950", method: "#bc8cff" };
  const sizeMap = { function: 7, class: 11, method: 6 };

  // ===== Init =====
  document.addEventListener("DOMContentLoaded", function () {
    loadStats();
    loadFileTree();
    loadGraphData();
    initSearch();
    initResizers();
    initToolbar();

    document.getElementById("popup-close").addEventListener("click", closePopup);
  });

  // ===== Stats =====
  function loadStats() {
    fetch("/agent/stats").then(r => r.json()).then(data => {
      document.getElementById("stat-nodes").textContent = (data.node_count || 0) + " 实体";
      document.getElementById("stat-edges").textContent = (data.edge_count || 0) + " 调用";
    }).catch(() => {});
  }

  // ===== File Tree =====
  function loadFileTree() {
    const container = document.getElementById("file-tree");
    fetch("/api/v1/file/tree").then(r => r.json()).then(resp => {
      if (!resp.success) { container.innerHTML = '<div class="tree-loading">加载失败</div>'; return; }
      container.innerHTML = "";
      renderTreeNode(container, resp.data, 0);
    }).catch(() => { container.innerHTML = '<div class="tree-loading">加载失败</div>'; });
  }

  function renderTreeNode(parent, node, depth) {
    if (!node.children && !node.is_dir) {
      // File
      const item = document.createElement("div");
      item.className = "tree-item";
      item.innerHTML = '<span class="tree-indent" style="width:' + (depth * 16) + 'px"></span>' +
        '<span class="tree-icon file">📄</span>' +
        '<span class="tree-name">' + esc(node.name) + '</span>';
      item.addEventListener("click", function () {
        document.querySelectorAll(".tree-item.active").forEach(el => el.classList.remove("active"));
        item.classList.add("active");
        loadFileContent(node.path);
      });
      parent.appendChild(item);
      return;
    }

    // Directory
    const item = document.createElement("div");
    item.className = "tree-item";
    item.innerHTML = '<span class="tree-indent" style="width:' + (depth * 16) + 'px"></span>' +
      '<span class="tree-icon dir">▸</span>' +
      '<span class="tree-name">' + esc(node.name) + '</span>';

    const childContainer = document.createElement("div");
    childContainer.className = "tree-children";

    item.addEventListener("click", function () {
      const icon = item.querySelector(".tree-icon");
      const isOpen = childContainer.classList.toggle("open");
      icon.textContent = isOpen ? "▾" : "▸";
    });

    parent.appendChild(item);
    parent.appendChild(childContainer);

    if (node.children) {
      // Sort: dirs first, then files
      const sorted = node.children.slice().sort((a, b) => {
        if (a.is_dir && !b.is_dir) return -1;
        if (!a.is_dir && b.is_dir) return 1;
        return a.name.localeCompare(b.name);
      });
      sorted.forEach(child => renderTreeNode(childContainer, child, depth + 1));
    }
  }

  // ===== File Content Viewer =====
  function loadFileContent(filePath, startLine, endLine) {
    const viewer = document.getElementById("code-viewer");
    const title = document.getElementById("code-title");
    title.textContent = filePath;

    let url = "/api/v1/file/lines?path=" + encodeURIComponent(filePath);
    if (startLine) url += "&start=" + startLine;
    if (endLine) url += "&end=" + endLine;

    fetch(url).then(r => r.json()).then(resp => {
      if (!resp.success) { viewer.innerHTML = '<div class="code-placeholder"><p>' + esc(resp.error) + '</p></div>'; return; }
      const data = resp.data;
      const ext = filePath.split(".").pop();
      const langMap = { go: "go", py: "python", js: "javascript", ts: "typescript", java: "java", c: "c", cpp: "cpp", h: "c", hpp: "cpp" };
      const lang = langMap[ext] || "";

      let html = '<div class="code-file-header"><span class="code-file-path">' + esc(filePath) + '</span>';
      html += '<span>(' + data.start_line + '-' + data.end_line + ' / ' + data.total_lines + ' 行)</span></div>';
      html += '<div class="code-lines">';

      const lines = data.lines || [];
      const hlStart = startLine || 0;
      const hlEnd = endLine || 0;

      for (let i = 0; i < lines.length; i++) {
        const lineNum = (data.start_line || 1) + i;
        const isHL = hlStart && hlEnd && lineNum >= hlStart && lineNum <= hlEnd;
        let content = esc(lines[i]);
        if (lang) {
          try { content = hljs.highlight(lines[i], { language: lang }).value; } catch (_) {}
        }
        html += '<div class="code-line' + (isHL ? ' highlighted' : '') + '">' +
          '<span class="line-number">' + lineNum + '</span>' +
          '<span class="line-content">' + content + '</span></div>';
      }
      html += '</div>';
      viewer.innerHTML = html;
    }).catch(() => {
      viewer.innerHTML = '<div class="code-placeholder"><p>加载文件失败</p></div>';
    });
  }

  // ===== Search =====
  function initSearch() {
    const input = document.getElementById("search-input");
    const results = document.getElementById("search-results");
    let timer = null;

    input.addEventListener("input", function () {
      clearTimeout(timer);
      const q = input.value.trim();
      if (q.length < 2) { results.classList.add("hidden"); return; }
      timer = setTimeout(() => doSearch(q), 300);
    });

    input.addEventListener("keydown", function (e) {
      if (e.key === "Escape") { results.classList.add("hidden"); }
    });

    document.addEventListener("click", function (e) {
      if (!results.contains(e.target) && e.target !== input) results.classList.add("hidden");
    });
  }

  function doSearch(query) {
    const results = document.getElementById("search-results");
    fetch("/api/v1/search/semantic", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: query, top_k: 8 }),
    }).then(r => r.json()).then(resp => {
      if (!resp.success || !resp.data || resp.data.length === 0) {
        results.innerHTML = '<div class="search-result-item"><span class="search-result-info">无结果</span></div>';
        results.classList.remove("hidden");
        return;
      }
      results.innerHTML = resp.data.map(r =>
        '<div class="search-result-item" data-id="' + esc(r.entity_id) + '">' +
        '<div class="search-result-name">' + esc(r.entity_id) + '</div>' +
        '<div class="search-result-info">' + esc(r.type || "") + ' · ' + esc(r.file_path || "") +
        ' · 相似度 ' + (r.score * 100).toFixed(0) + '%</div></div>'
      ).join("");
      results.classList.remove("hidden");

      results.querySelectorAll(".search-result-item").forEach(item => {
        item.addEventListener("click", function () {
          const id = item.dataset.id;
          results.classList.add("hidden");
          focusNode(id);
        });
      });
    }).catch(() => {});
  }

  // ===== Graph =====
  function loadGraphData() {
    fetch("/agent/graph/data").then(r => r.json()).then(data => {
      state.graphData = data;
      renderGraph(data);
    }).catch(err => console.error("Graph load failed:", err));
  }

  function renderGraph(data) {
    const viewport = document.getElementById("graph-viewport");
    const svg = d3.select("#graph-svg");
    svg.selectAll("*").remove();

    if (!data.nodes || data.nodes.length === 0) {
      svg.append("text").attr("x", "50%").attr("y", "50%").attr("text-anchor", "middle")
        .attr("fill", "#6e7681").attr("font-size", "14px")
        .text("暂无数据，请先索引代码库");
      return;
    }

    const width = viewport.clientWidth;
    const height = viewport.clientHeight;

    // Tooltip
    let tooltip = d3.select(".graph-tooltip");
    if (tooltip.empty()) {
      tooltip = d3.select("body").append("div").attr("class", "graph-tooltip").style("display", "none");
    }

    const g = svg.append("g");
    state.gElement = g;

    // Zoom
    state.zoomBehavior = d3.zoom().scaleExtent([0.1, 5])
      .on("zoom", event => g.attr("transform", event.transform));
    svg.call(state.zoomBehavior);

    // Filter valid edges
    const nodeIds = new Set(data.nodes.map(n => n.id));
    const edges = data.edges.filter(e => nodeIds.has(e.source) && nodeIds.has(e.target));

    // Arrow marker
    svg.append("defs").append("marker").attr("id", "arrow")
      .attr("viewBox", "0 -5 10 10").attr("refX", 20).attr("refY", 0)
      .attr("markerWidth", 5).attr("markerHeight", 5).attr("orient", "auto")
      .append("path").attr("d", "M0,-4L10,0L0,4").attr("fill", "#30363d");

    // Simulation
    state.simulation = d3.forceSimulation(data.nodes)
      .force("link", d3.forceLink(edges).id(d => d.id).distance(80))
      .force("charge", d3.forceManyBody().strength(-200))
      .force("center", d3.forceCenter(width / 2, height / 2))
      .force("collision", d3.forceCollide().radius(20));

    // Edges
    const link = g.append("g").selectAll("line").data(edges).join("line")
      .attr("stroke", "#30363d").attr("stroke-width", 1).attr("marker-end", "url(#arrow)");

    // Nodes
    const node = g.append("g").selectAll("circle").data(data.nodes).join("circle")
      .attr("r", d => sizeMap[d.type] || 7)
      .attr("fill", d => colorMap[d.type] || "#58a6ff")
      .attr("stroke", "#0d1117").attr("stroke-width", 1.5)
      .attr("cursor", "pointer")
      .call(d3.drag().on("start", dragStart).on("drag", dragging).on("end", dragEnd));

    // Labels
    const label = g.append("g").selectAll("text").data(data.nodes).join("text")
      .text(d => d.name).attr("font-size", "10px").attr("fill", "#6e7681")
      .attr("dx", 12).attr("dy", 3).attr("pointer-events", "none");

    // Hover
    node.on("mouseover", (event, d) => {
      tooltip.style("display", "block").html(
        '<div class="tooltip-name">' + esc(d.name) + '</div>' +
        '<div class="tooltip-type">' + esc(d.type) + '</div>' +
        '<div class="tooltip-file">' + esc(d.file_path || "") + ':' + (d.start_line || "") + '</div>' +
        (d.signature ? '<div class="tooltip-sig">' + esc(d.signature) + '</div>' : "")
      );
    }).on("mousemove", event => {
      tooltip.style("left", (event.pageX + 14) + "px").style("top", (event.pageY - 8) + "px");
    }).on("mouseout", () => tooltip.style("display", "none"));

    // Click
    node.on("click", (event, d) => {
      event.stopPropagation();
      showNodePopup(d);
    });

    svg.on("click", () => closePopup());

    // Tick
    state.simulation.on("tick", () => {
      link.attr("x1", d => d.source.x).attr("y1", d => d.source.y)
        .attr("x2", d => d.target.x).attr("y2", d => d.target.y);
      node.attr("cx", d => d.x).attr("cy", d => d.y);
      label.attr("x", d => d.x).attr("y", d => d.y);
    });

    function dragStart(event) {
      if (!event.active) state.simulation.alphaTarget(0.3).restart();
      event.subject.fx = event.subject.x;
      event.subject.fy = event.subject.y;
    }
    function dragging(event) {
      event.subject.fx = event.x;
      event.subject.fy = event.y;
    }
    function dragEnd(event) {
      if (!event.active) state.simulation.alphaTarget(0);
      event.subject.fx = null;
      event.subject.fy = null;
    }
  }

  // ===== Node Popup =====
  function showNodePopup(d) {
    state.selectedNode = d;
    const popup = document.getElementById("node-popup");
    document.getElementById("popup-title").textContent = d.name;
    document.getElementById("popup-body").innerHTML =
      '<div class="detail-row"><span class="detail-label">类型</span><span class="detail-value">' + esc(d.type) + '</span></div>' +
      '<div class="detail-row"><span class="detail-label">ID</span><span class="detail-value">' + esc(d.id) + '</span></div>' +
      '<div class="detail-row"><span class="detail-label">文件</span><span class="detail-value">' + esc(d.file_path || "") + '</span></div>' +
      '<div class="detail-row"><span class="detail-label">行号</span><span class="detail-value">' + (d.start_line || "") + ' - ' + (d.end_line || "") + '</span></div>' +
      (d.signature ? '<div class="detail-row"><span class="detail-label">签名</span><span class="detail-value">' + esc(d.signature) + '</span></div>' : "");

    popup.classList.remove("hidden");

    // Bind actions
    document.getElementById("popup-view-code").onclick = function () {
      closePopup();
      if (d.file_path) loadFileContent(d.file_path, d.start_line, d.end_line);
    };
    document.getElementById("popup-view-callers").onclick = function () {
      closePopup();
      highlightRelated(d.id, "callers");
    };
    document.getElementById("popup-view-callees").onclick = function () {
      closePopup();
      highlightRelated(d.id, "callees");
    };
  }

  function closePopup() {
    document.getElementById("node-popup").classList.add("hidden");
  }

  // ===== Highlight Related Nodes =====
  function highlightRelated(entityId, direction) {
    const url = direction === "callers"
      ? "/api/v1/callers/" + encodeURIComponent(entityId) + "?depth=1"
      : "/api/v1/callees/" + encodeURIComponent(entityId) + "?depth=1";

    fetch(url).then(r => r.json()).then(resp => {
      if (!resp.success || !resp.data) return;
      const relatedIds = new Set(resp.data.map(n => n.id));
      relatedIds.add(entityId);

      // Dim all, highlight related
      d3.selectAll("#graph-svg circle")
        .attr("opacity", d => relatedIds.has(d.id) ? 1 : 0.15)
        .attr("r", d => relatedIds.has(d.id) ? (sizeMap[d.type] || 7) + 3 : sizeMap[d.type] || 7);

      d3.selectAll("#graph-svg line")
        .attr("opacity", d => {
          const src = typeof d.source === "object" ? d.source.id : d.source;
          const tgt = typeof d.target === "object" ? d.target.id : d.target;
          return relatedIds.has(src) && relatedIds.has(tgt) ? 1 : 0.05;
        })
        .attr("stroke", d => {
          const src = typeof d.source === "object" ? d.source.id : d.source;
          const tgt = typeof d.target === "object" ? d.target.id : d.target;
          return relatedIds.has(src) && relatedIds.has(tgt) ? "#58a6ff" : "#30363d";
        });

      d3.selectAll("#graph-svg text")
        .attr("opacity", d => relatedIds.has(d.id) ? 1 : 0.1);

      document.getElementById("graph-title").textContent =
        direction === "callers" ? entityId + " 的调用者" : entityId + " 的被调用者";

      // Click anywhere to reset
      d3.select("#graph-svg").on("click.reset", function () {
        resetHighlight();
        d3.select("#graph-svg").on("click.reset", null);
      });
    });
  }

  function resetHighlight() {
    d3.selectAll("#graph-svg circle").attr("opacity", 1).attr("r", d => sizeMap[d.type] || 7);
    d3.selectAll("#graph-svg line").attr("opacity", 1).attr("stroke", "#30363d");
    d3.selectAll("#graph-svg text").attr("opacity", 1);
    document.getElementById("graph-title").textContent = "调用图谱";
  }

  function focusNode(entityId) {
    if (!state.graphData) return;
    const node = state.graphData.nodes.find(n => n.id === entityId);
    if (node && node.x != null) {
      const svg = d3.select("#graph-svg");
      const viewport = document.getElementById("graph-viewport");
      const w = viewport.clientWidth;
      const h = viewport.clientHeight;

      svg.transition().duration(500).call(
        state.zoomBehavior.transform,
        d3.zoomIdentity.translate(w / 2 - node.x * 1.5, h / 2 - node.y * 1.5).scale(1.5)
      );

      // Flash effect
      d3.selectAll("#graph-svg circle")
        .filter(d => d.id === entityId)
        .transition().duration(200).attr("r", 16)
        .transition().duration(400).attr("r", sizeMap[node.type] || 7);

      showNodePopup(node);
    }
  }

  // ===== Toolbar =====
  function initToolbar() {
    document.getElementById("btn-reset").addEventListener("click", function () {
      resetHighlight();
      if (state.graphData) renderGraph(state.graphData);
    });

    document.getElementById("btn-zoom-fit").addEventListener("click", function () {
      const svg = d3.select("#graph-svg");
      svg.transition().duration(300).call(state.zoomBehavior.transform, d3.zoomIdentity);
    });

    document.getElementById("btn-zoom-in").addEventListener("click", function () {
      d3.select("#graph-svg").transition().duration(200).call(state.zoomBehavior.scaleBy, 1.3);
    });

    document.getElementById("btn-zoom-out").addEventListener("click", function () {
      d3.select("#graph-svg").transition().duration(200).call(state.zoomBehavior.scaleBy, 0.7);
    });

    document.getElementById("graph-type-filter").addEventListener("change", function () {
      if (!state.graphData) return;
      const type = this.value;
      if (type === "all") { renderGraph(state.graphData); return; }

      const filtered = state.graphData.nodes.filter(n => n.type === type);
      const ids = new Set(filtered.map(n => n.id));
      const edges = state.graphData.edges.filter(e => ids.has(e.source) || ids.has(e.target) || ids.has(e.source?.id) || ids.has(e.target?.id));
      renderGraph({ nodes: filtered, edges: edges });
    });

    // Panel toggles
    document.getElementById("toggle-left").addEventListener("click", function () {
      const panel = document.getElementById("panel-left");
      const resizer = document.getElementById("resizer-left");
      const isHidden = panel.style.display === "none";
      panel.style.display = isHidden ? "" : "none";
      resizer.style.display = isHidden ? "" : "none";
      this.textContent = isHidden ? "◀" : "▶";
    });

    document.getElementById("toggle-right").addEventListener("click", function () {
      const panel = document.getElementById("panel-right");
      const resizer = document.getElementById("resizer-right");
      const isHidden = panel.style.display === "none";
      panel.style.display = isHidden ? "" : "none";
      resizer.style.display = isHidden ? "" : "none";
      this.textContent = isHidden ? "▶" : "◀";
    });
  }

  // ===== Resizers =====
  function initResizers() {
    makeResizable("resizer-left", "panel-left", "left");
    makeResizable("resizer-right", "panel-right", "right");
  }

  function makeResizable(resizerId, panelId, side) {
    const resizer = document.getElementById(resizerId);
    const panel = document.getElementById(panelId);
    let startX, startWidth;

    resizer.addEventListener("mousedown", function (e) {
      startX = e.clientX;
      startWidth = panel.getBoundingClientRect().width;
      resizer.classList.add("active");
      document.addEventListener("mousemove", onMouseMove);
      document.addEventListener("mouseup", onMouseUp);
      e.preventDefault();
    });

    function onMouseMove(e) {
      const diff = e.clientX - startX;
      const newWidth = side === "left" ? startWidth + diff : startWidth - diff;
      if (newWidth >= 150 && newWidth <= 600) {
        panel.style.width = newWidth + "px";
      }
    }

    function onMouseUp() {
      resizer.classList.remove("active");
      document.removeEventListener("mousemove", onMouseMove);
      document.removeEventListener("mouseup", onMouseUp);
    }
  }

  // ===== Utils =====
  function esc(text) {
    const div = document.createElement("div");
    div.textContent = text || "";
    return div.innerHTML;
  }
})();
