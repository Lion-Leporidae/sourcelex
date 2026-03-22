// RepoMind Web UI

(function () {
  "use strict";

  // --- State ---
  const state = {
    history: [],
    graphVisible: false,
    graphData: null,
    simulation: null,
    sending: false,
  };

  // --- DOM refs ---
  const $messages = document.getElementById("messages");
  const $input = document.getElementById("user-input");
  const $sendBtn = document.getElementById("btn-send");
  const $graphPanel = document.getElementById("graph-panel");
  const $chatPanel = document.getElementById("chat-panel");
  const $statsModal = document.getElementById("stats-modal");
  const $statsBody = document.getElementById("stats-body");
  const $graphFilter = document.getElementById("graph-filter");

  // --- Markdown config ---
  marked.setOptions({
    highlight: function (code, lang) {
      if (lang && hljs.getLanguage(lang)) {
        return hljs.highlight(code, { language: lang }).value;
      }
      return hljs.highlightAuto(code).value;
    },
    breaks: true,
  });

  // --- Init ---
  function init() {
    $sendBtn.addEventListener("click", sendMessage);
    $input.addEventListener("keydown", function (e) {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        sendMessage();
      }
    });
    $input.addEventListener("input", autoResize);

    document.getElementById("btn-stats").addEventListener("click", showStats);
    document
      .getElementById("stats-close")
      .addEventListener("click", function () {
        $statsModal.classList.add("hidden");
      });
    $statsModal.addEventListener("click", function (e) {
      if (e.target === $statsModal) $statsModal.classList.add("hidden");
    });

    document
      .getElementById("btn-graph")
      .addEventListener("click", toggleGraph);
    document
      .getElementById("btn-graph-reset")
      .addEventListener("click", resetGraph);
    $graphFilter.addEventListener("change", filterGraph);

    document.querySelectorAll(".quick-btn").forEach(function (btn) {
      btn.addEventListener("click", function () {
        $input.value = btn.dataset.query;
        sendMessage();
      });
    });

    $input.focus();
  }

  // --- Auto-resize textarea ---
  function autoResize() {
    $input.style.height = "auto";
    $input.style.height = Math.min($input.scrollHeight, 120) + "px";
  }

  // --- Send message ---
  function sendMessage() {
    const text = $input.value.trim();
    if (!text || state.sending) return;

    state.sending = true;
    $sendBtn.disabled = true;

    // Hide welcome
    const welcome = $messages.querySelector(".welcome");
    if (welcome) welcome.remove();

    // Add user message
    appendMessage("user", text);
    state.history.push({ role: "user", content: text });

    $input.value = "";
    $input.style.height = "auto";

    // Stream response
    streamChat(text);
  }

  // --- Stream chat via SSE ---
  function streamChat(message) {
    const assistantEl = appendMessage("assistant", "");
    const contentEl = assistantEl.querySelector(".message-content");
    let fullContent = "";

    fetch("/agent/chat/stream", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        message: message,
        history: state.history.slice(0, -1),
      }),
    })
      .then(function (response) {
        if (!response.ok) {
          return response.json().then(function (err) {
            throw new Error(err.error || err.Error || "请求失败");
          });
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";

        function processStream() {
          return reader.read().then(function (result) {
            if (result.done) {
              finishStream();
              return;
            }

            buffer += decoder.decode(result.value, { stream: true });
            const lines = buffer.split("\n");
            buffer = lines.pop() || "";

            let eventType = "";
            for (let i = 0; i < lines.length; i++) {
              const line = lines[i].trim();
              if (line.startsWith("event: ")) {
                eventType = line.substring(7);
              } else if (line.startsWith("data: ")) {
                const data = line.substring(6);
                try {
                  const evt = JSON.parse(data);
                  handleStreamEvent(
                    eventType || evt.type,
                    evt,
                    contentEl,
                    function (c) {
                      fullContent += c;
                    }
                  );
                } catch (_e) {
                  // ignore parse errors
                }
              }
            }

            return processStream();
          });
        }

        return processStream();
      })
      .catch(function (err) {
        contentEl.innerHTML = renderMarkdown(
          "**错误**: " + (err.message || "网络请求失败，请检查服务器是否运行")
        );
        finishStream();
      });

    function finishStream() {
      if (fullContent) {
        state.history.push({ role: "assistant", content: fullContent });
      }
      // Remove any remaining status messages
      document.querySelectorAll(".message-status").forEach(function (el) {
        el.remove();
      });
      state.sending = false;
      $sendBtn.disabled = false;
      scrollToBottom();
    }
  }

  function handleStreamEvent(type, evt, contentEl, appendContent) {
    switch (type) {
      case "status":
        showStatus(evt.message);
        break;
      case "chunk":
        // Remove status messages
        document.querySelectorAll(".message-status").forEach(function (el) {
          el.remove();
        });
        appendContent(evt.content);
        contentEl.innerHTML = renderMarkdown(
          contentEl._rawContent
            ? (contentEl._rawContent += evt.content)
            : (contentEl._rawContent = evt.content)
        );
        scrollToBottom();
        break;
      case "done":
        break;
      case "error":
        contentEl.innerHTML = renderMarkdown("**错误**: " + evt.message);
        scrollToBottom();
        break;
    }
  }

  // --- Message rendering ---
  function appendMessage(role, content) {
    const div = document.createElement("div");
    div.className = "message message-" + role;

    if (role === "user") {
      div.innerHTML = '<div class="message-content">' + escapeHtml(content) + "</div>";
    } else {
      div.innerHTML =
        '<div class="message-avatar">RM</div>' +
        '<div class="message-content">' +
        (content ? renderMarkdown(content) : '<span class="status-dot"></span>') +
        "</div>";
    }

    $messages.appendChild(div);
    scrollToBottom();
    return div;
  }

  function showStatus(message) {
    // Remove previous status
    document.querySelectorAll(".message-status").forEach(function (el) {
      el.remove();
    });

    const div = document.createElement("div");
    div.className = "message-status";
    div.innerHTML =
      '<div class="status-content"><span class="status-dot"></span>' +
      escapeHtml(message) +
      "</div>";
    $messages.appendChild(div);
    scrollToBottom();
  }

  function renderMarkdown(text) {
    if (!text) return "";
    return marked.parse(text);
  }

  function escapeHtml(text) {
    const div = document.createElement("div");
    div.textContent = text;
    return div.innerHTML;
  }

  function scrollToBottom() {
    $messages.scrollTop = $messages.scrollHeight;
  }

  // --- Stats ---
  function showStats() {
    $statsModal.classList.remove("hidden");
    $statsBody.innerHTML = '<div class="stats-loading">加载中...</div>';

    fetch("/agent/stats")
      .then(function (r) {
        return r.json();
      })
      .then(function (data) {
        $statsBody.innerHTML =
          '<div class="stats-grid">' +
          '<div class="stat-card"><div class="stat-value">' +
          (data.node_count || 0) +
          '</div><div class="stat-label">实体</div></div>' +
          '<div class="stat-card"><div class="stat-value">' +
          (data.edge_count || 0) +
          '</div><div class="stat-label">关系</div></div>' +
          '<div class="stat-card"><div class="stat-value">' +
          (data.vector_count || 0) +
          '</div><div class="stat-label">向量</div></div>' +
          "</div>";
      })
      .catch(function () {
        $statsBody.innerHTML =
          '<div class="stats-loading">加载失败，请检查服务状态</div>';
      });
  }

  // --- Graph ---
  function toggleGraph() {
    state.graphVisible = !state.graphVisible;
    const btn = document.getElementById("btn-graph");

    if (state.graphVisible) {
      btn.classList.add("active");
      $graphPanel.classList.remove("hidden");
      $chatPanel.classList.add("with-graph");
      loadGraphData();
    } else {
      btn.classList.remove("active");
      $graphPanel.classList.add("hidden");
      $chatPanel.classList.remove("with-graph");
    }
  }

  function loadGraphData() {
    fetch("/agent/graph/data")
      .then(function (r) {
        return r.json();
      })
      .then(function (data) {
        state.graphData = data;
        renderGraph(data);
      })
      .catch(function (err) {
        console.error("Failed to load graph:", err);
      });
  }

  function renderGraph(data) {
    const container = document.getElementById("graph-container");
    const svg = d3.select("#graph-svg");
    svg.selectAll("*").remove();

    if (!data.nodes || data.nodes.length === 0) {
      svg
        .append("text")
        .attr("x", "50%")
        .attr("y", "50%")
        .attr("text-anchor", "middle")
        .attr("fill", "#8b949e")
        .attr("font-size", "14px")
        .text("暂无数据，请先运行 repomind store 索引代码库");
      return;
    }

    const width = container.clientWidth;
    const height = container.clientHeight;

    const colorMap = {
      function: "#58a6ff",
      class: "#3fb950",
      method: "#bc8cff",
    };

    // Tooltip
    const tooltip = d3
      .select("body")
      .append("div")
      .attr("class", "graph-tooltip")
      .style("display", "none");

    // SVG setup with zoom
    const g = svg.append("g");

    const zoom = d3
      .zoom()
      .scaleExtent([0.1, 4])
      .on("zoom", function (event) {
        g.attr("transform", event.transform);
      });

    svg.call(zoom);

    // Build node ID set for filtering edges
    const nodeIds = new Set(data.nodes.map(function (n) { return n.id; }));
    const validEdges = data.edges.filter(function (e) {
      return nodeIds.has(e.source) && nodeIds.has(e.target);
    });

    // Arrow markers
    svg
      .append("defs")
      .append("marker")
      .attr("id", "arrowhead")
      .attr("viewBox", "0 -5 10 10")
      .attr("refX", 20)
      .attr("refY", 0)
      .attr("markerWidth", 6)
      .attr("markerHeight", 6)
      .attr("orient", "auto")
      .append("path")
      .attr("d", "M0,-5L10,0L0,5")
      .attr("fill", "#30363d");

    // Force simulation
    state.simulation = d3
      .forceSimulation(data.nodes)
      .force(
        "link",
        d3
          .forceLink(validEdges)
          .id(function (d) { return d.id; })
          .distance(100)
      )
      .force("charge", d3.forceManyBody().strength(-300))
      .force("center", d3.forceCenter(width / 2, height / 2))
      .force("collision", d3.forceCollide().radius(30));

    // Edges
    const link = g
      .append("g")
      .selectAll("line")
      .data(validEdges)
      .join("line")
      .attr("stroke", "#30363d")
      .attr("stroke-width", 1.5)
      .attr("marker-end", "url(#arrowhead)");

    // Nodes
    const node = g
      .append("g")
      .selectAll("circle")
      .data(data.nodes)
      .join("circle")
      .attr("r", function (d) {
        return d.type === "class" ? 10 : 7;
      })
      .attr("fill", function (d) {
        return colorMap[d.type] || "#58a6ff";
      })
      .attr("stroke", "#0d1117")
      .attr("stroke-width", 2)
      .attr("cursor", "pointer")
      .call(
        d3
          .drag()
          .on("start", dragStarted)
          .on("drag", dragged)
          .on("end", dragEnded)
      );

    // Labels
    const label = g
      .append("g")
      .selectAll("text")
      .data(data.nodes)
      .join("text")
      .text(function (d) { return d.name; })
      .attr("font-size", "11px")
      .attr("fill", "#8b949e")
      .attr("dx", 14)
      .attr("dy", 4)
      .attr("pointer-events", "none");

    // Tooltip events
    node
      .on("mouseover", function (event, d) {
        tooltip.style("display", "block").html(
          '<div class="tooltip-name">' +
            escapeHtml(d.name) +
            "</div>" +
            '<div class="tooltip-info">' +
            escapeHtml(d.type) +
            " &middot; " +
            escapeHtml(d.file_path || "") +
            "</div>" +
            (d.signature
              ? '<div class="tooltip-info" style="margin-top:4px;font-family:monospace;font-size:11px">' +
                escapeHtml(d.signature) +
                "</div>"
              : "")
        );
      })
      .on("mousemove", function (event) {
        tooltip
          .style("left", event.pageX + 12 + "px")
          .style("top", event.pageY - 10 + "px");
      })
      .on("mouseout", function () {
        tooltip.style("display", "none");
      })
      .on("click", function (_event, d) {
        $input.value = d.name + " 这个函数/类是做什么的？它的调用关系是怎样的？";
        if (state.graphVisible) toggleGraph();
        $input.focus();
      });

    // Simulation tick
    state.simulation.on("tick", function () {
      link
        .attr("x1", function (d) { return d.source.x; })
        .attr("y1", function (d) { return d.source.y; })
        .attr("x2", function (d) { return d.target.x; })
        .attr("y2", function (d) { return d.target.y; });

      node
        .attr("cx", function (d) { return d.x; })
        .attr("cy", function (d) { return d.y; });

      label
        .attr("x", function (d) { return d.x; })
        .attr("y", function (d) { return d.y; });
    });

    function dragStarted(event) {
      if (!event.active) state.simulation.alphaTarget(0.3).restart();
      event.subject.fx = event.subject.x;
      event.subject.fy = event.subject.y;
    }

    function dragged(event) {
      event.subject.fx = event.x;
      event.subject.fy = event.y;
    }

    function dragEnded(event) {
      if (!event.active) state.simulation.alphaTarget(0);
      event.subject.fx = null;
      event.subject.fy = null;
    }
  }

  function resetGraph() {
    if (state.graphData) {
      renderGraph(state.graphData);
    }
  }

  function filterGraph() {
    if (!state.graphData) return;
    const type = $graphFilter.value;

    if (type === "all") {
      renderGraph(state.graphData);
      return;
    }

    var filteredNodes = state.graphData.nodes.filter(function (n) {
      return n.type === type;
    });
    var nodeIds = new Set(filteredNodes.map(function (n) { return n.id; }));
    var filteredEdges = state.graphData.edges.filter(function (e) {
      return nodeIds.has(e.source) || nodeIds.has(e.target) ||
             nodeIds.has(e.source.id) || nodeIds.has(e.target.id);
    });

    renderGraph({ nodes: filteredNodes, edges: filteredEdges });
  }

  // --- Start ---
  document.addEventListener("DOMContentLoaded", init);
})();
