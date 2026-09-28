import QtQuick
import QtQuick.Controls
import Quickshell
import Quickshell.Io
import qs.Ui
import qs.Commons

Panel {
  id: root
  moduleName: "marcus.hue"
  ipcTarget: "marcus.hue"



  readonly property string hueBin: {
    var configured = String(setting("command", "") || "")
    if (configured.length > 0) return configured
    var home = String(Quickshell.env("HOME") || "")
    return home.length > 0 ? home + "/.local/bin/hue" : "hue"
  }
  readonly property string uiFont: bar ? bar.fontFamily : Style.font.family
  readonly property color uiFg: bar ? bar.foreground : Color.foreground
  readonly property color uiDim: Qt.darker(uiFg, 1.45)
  readonly property color uiUrgent: bar ? bar.urgent : Color.urgent
  readonly property var swatches: [
    { hue: 0, color: "#d64545" },
    { hue: 28, color: "#e07a32" },
    { hue: 48, color: "#e2b33a" },
    { hue: 120, color: "#3aaa5a" },
    { hue: 185, color: "#2aa8c4" },
    { hue: 222, color: "#3a6fe0" },
    { hue: 275, color: "#8a4fd4" },
    { hue: 320, color: "#d24b8a" }
  ]
  readonly property int listCap: {
    var avail = panel.availableCardHeight
    if (!(avail > 0)) return Style.space(480)
    return Math.max(Style.space(180), Math.round(avail - Style.space(170)))
  }

  property var model: ({ paired: false, bridges: [], groups: [], anyOn: false })
  property var overrides: ({})
  property int revision: 0
  property string lastSnapshot: ""
  property string lastError: ""
  property string actionStdout: ""
  property string actionStderr: ""
  property var actionQueue: []
  property bool loading: false
  property bool pairing: false
  property bool actionRunning: false
  property bool dragging: false
  property bool forgetArmed: false
  property bool refreshQueued: false
  property string openRoomId: ""
  property int commandEpoch: 0
  property int inflightSnapshotEpoch: 0

  readonly property bool paired: !!(model && model.paired)
  readonly property var groups: (model && model.groups) ? model.groups : []
  readonly property var bridges: (model && model.bridges) ? model.bridges : []
  readonly property bool anyOn: {
    var touch = root.revision
    var rooms = root.groups
    for (var i = 0; i < rooms.length; i++) {
      var lights = rooms[i].lights || []
      for (var j = 0; j < lights.length; j++) {
        if (root.shown(lights[j].id, "on", lights[j].on === true)) return true
      }
      var switches = rooms[i].switches || []
      for (var k = 0; k < switches.length; k++) {
        if (touch >= 0 && root.shown(switches[k].id, "on", switches[k].on === true)) return true
      }
    }
    return false
  }
  readonly property string statusText: {
    if (root.pairing) return "Press the link button"
    if (!root.paired) return root.loading && root.bridges.length === 0 ? "Looking for a bridge" : "Not paired"
    if (root.model && root.model.authorized === false) return "Key rejected"
    if (root.model && root.model.reachable === false) return "Bridge unreachable"
    var bridge = root.model && root.model.bridge
    return bridge && bridge.name ? bridge.name : "Hue Bridge"
  }

  function shown(id, key, fallback) {
    var touch = root.revision
    var bucket = root.overrides[id]
    if (touch >= 0 && bucket && bucket[key] !== undefined) return bucket[key]
    return fallback
  }

  function remember(id, patch) {
    var next = {}
    var current = root.overrides || {}
    for (var key in current) next[key] = current[key]
    var row = {}
    var prev = next[id] || {}
    for (var kept in prev) row[kept] = prev[kept]
    for (var name in patch) row[name] = patch[name]
    next[id] = row
    root.overrides = next
    root.revision += 1
  }

  function roomById(id) {
    var touch = root.revision
    var groups = root.groups || []
    for (var i = 0; i < groups.length; i++) {
      if (touch >= 0 && groups[i] && groups[i].id === id) return groups[i]
    }
    return null
  }

  readonly property var openRoom: roomById(openRoomId)

  function showRoom(id) {
    root.openRoomId = id
    scroller.contentY = 0
  }

  function refresh() {
    if (snapshotProcess.running || root.actionRunning || root.dragging || root.pairing
        || refreshDebounce.running || (root.actionQueue && root.actionQueue.length > 0)) {
      root.refreshQueued = true
      return
    }
    root.refreshQueued = false
    root.inflightSnapshotEpoch = root.commandEpoch
    snapshotProcess.command = [root.hueBin, "snapshot"]
    root.loading = true
    snapshotProcess.running = true
  }

  function applySnapshot(text) {
    var trimmed = String(text || "").trim()
    if (!trimmed) return
    // A snapshot that started before the latest command still describes the
    // old bridge state. Applying it snaps the switches back.
    if (root.inflightSnapshotEpoch !== root.commandEpoch) return
    if (root.actionRunning || (root.actionQueue && root.actionQueue.length > 0)) return
    if (trimmed === root.lastSnapshot) {
      root.overrides = ({})
      root.revision += 1
      return
    }
    var data = JSON.parse(trimmed)
    root.overrides = ({})
    root.model = data
    root.lastSnapshot = trimmed
    root.lastError = data.error ? String(data.error) : ""
    root.revision += 1
    if (root.openRoomId !== "") {
      var groups = data.groups || []
      var still = false
      for (var i = 0; i < groups.length; i++) {
        if (groups[i] && groups[i].id === root.openRoomId) still = true
      }
      if (!still) root.openRoomId = ""
    }
  }

  function enqueue(args) {
    root.commandEpoch += 1
    var queue = []
    var current = root.actionQueue || []
    for (var i = 0; i < current.length; i++) queue.push(current[i])
    queue.push(args)
    root.actionQueue = queue
    root.pump()
  }

  function pump() {
    if (root.actionRunning || !root.actionQueue || root.actionQueue.length === 0) return
    var queue = []
    for (var i = 0; i < root.actionQueue.length; i++) queue.push(root.actionQueue[i])
    var args = queue.shift()
    root.actionQueue = queue
    root.actionStdout = ""
    root.actionStderr = ""
    actionProcess.command = args
    root.actionRunning = true
    actionProcess.running = true
  }

  function finishAction(exitCode) {
    var stdout = String(root.actionStdout || actionOut.text || "")
    var stderr = String(root.actionStderr || actionErr.text || "")
    root.actionRunning = false
    root.pairing = false
    if (exitCode !== 0) {
      var message = stderr.trim() || ("Could not run " + root.hueBin)
      try {
        var parsed = JSON.parse(stdout)
        if (parsed && parsed.error) message = String(parsed.error)
      } catch (e) {}
      root.lastError = message
      root.overrides = ({})
      root.revision += 1
    }
    if (root.actionQueue.length > 0) root.pump()
    else refreshDebounce.restart()
  }

  function pair(ip, id, name) {
    ip = String(ip || "").trim()
    if (!ip || root.pairing) return
    root.pairing = true
    root.lastError = ""
    var args = [root.hueBin, "pair", "--ip", ip, "--wait", "30s"]
    if (id) args.push("--id", String(id))
    if (name) args.push("--name", String(name))
    root.enqueue(args)
  }

  function clearShown(id, key) {
    var current = root.overrides || {}
    if (!current[id] || current[id][key] === undefined) return
    var next = {}
    for (var name in current) next[name] = current[name]
    var row = {}
    var prev = next[id]
    for (var kept in prev) {
      if (kept !== key) row[kept] = prev[kept]
    }
    next[id] = row
    root.overrides = next
    root.revision += 1
  }

  function toggleOn(kind, id, on) {
    root.remember(id, { on: on })
    root.enqueue([root.hueBin, "set", "--kind", kind, "--id", id, "--on", on ? "true" : "false"])
  }

  // The bridge turns every lamp in the room off with the room. Mirror that on
  // each device immediately, and put their previous state back if the room is
  // turned on again before the next refresh.
  function toggleRoom(room, on) {
    if (!room || !room.controlId) return
    root.remember(room.controlId, { on: on })
    var devices = []
    var lights = room.lights || []
    var switches = room.switches || []
    for (var i = 0; i < lights.length; i++) devices.push(lights[i])
    for (var j = 0; j < switches.length; j++) devices.push(switches[j])
    for (var k = 0; k < devices.length; k++) {
      var device = devices[k]
      if (!device || !device.id) continue
      if (on) root.clearShown(device.id, "on")
      else root.remember(device.id, { on: false })
    }
    root.enqueue([root.hueBin, "set", "--kind", "grouped_light", "--id", room.controlId, "--on", on ? "true" : "false"])
  }

  function setBrightness(kind, id, value) {
    var next = Math.round(value)
    root.remember(id, { brightness: next, on: true })
    root.enqueue([root.hueBin, "set", "--kind", kind, "--id", id, "--brightness", String(next)])
  }

  function setHueSat(id, hue, sat) {
    var h = Math.round(hue)
    var s = Math.max(0, Math.min(100, Math.round(sat)))
    root.remember(id, { hue: h, saturation: s, on: true })
    root.enqueue([root.hueBin, "set", "--kind", "light", "--id", id, "--hue", String(h), "--saturation", String(s)])
  }

  function setCt(id, mirek) {
    var next = Math.round(mirek)
    root.remember(id, { ct: next, on: true })
    root.enqueue([root.hueBin, "set", "--kind", "light", "--id", id, "--ct", String(next)])
  }

  function activateScene(scene) {
    if (!scene || !scene.id) return
    root.enqueue([root.hueBin, "scene", "--kind", scene.kind || "scene", "--id", scene.id])
  }

  function forget() {
    if (!root.forgetArmed) {
      root.forgetArmed = true
      forgetTimer.restart()
      return
    }
    root.forgetArmed = false
    root.enqueue([root.hueBin, "forget"])
  }

  // One snapshot at startup so a paired bridge lights the icon. After that,
  // poll only while the panel is open or a bridge is paired. An unpaired
  // closed panel has nothing to refresh.
  Component.onCompleted: root.refresh()
  Timer {
    interval: root.opened ? 8000 : 20000
    repeat: true
    running: root.opened || root.paired
    onTriggered: root.refresh()
  }

  Timer {
    id: refreshDebounce
    interval: 450
    onTriggered: root.refresh()
  }

  Timer {
    id: forgetTimer
    interval: 4000
    onTriggered: root.forgetArmed = false
  }

  onOpenedChanged: if (opened) refresh()

  // Holds one TLS session to the bridge. Each click then skips the handshake.
  Process {
    id: hueServer
    running: true
    command: [root.hueBin, "serve"]
  }

  Process {
    id: snapshotProcess
    running: false
    stdout: StdioCollector {
      id: snapOut
      waitForEnd: true
    }
    stderr: StdioCollector {
      id: snapErr
      waitForEnd: true
    }
    onExited: function(exitCode) {
      root.loading = false
      var stdout = String(snapOut.text || "")
      var stderr = String(snapErr.text || "").trim()
      if (exitCode === 0) {
        try {
          root.applySnapshot(stdout)
        } catch (e) {
          root.lastError = "Could not read Hue status"
        }
      } else {
        root.lastError = stderr || stdout.trim() || ("Could not run " + root.hueBin)
      }
      if (root.refreshQueued) root.refresh()
    }
  }

  Process {
    id: actionProcess
    running: false
    stdout: StdioCollector {
      id: actionOut
      waitForEnd: true
      onStreamFinished: root.actionStdout = text
    }
    stderr: StdioCollector {
      id: actionErr
      waitForEnd: true
      onStreamFinished: root.actionStderr = text
    }
    onExited: function(exitCode) {
      Qt.callLater(function() { root.finishAction(exitCode) })
    }
  }

  // The bar slot takes its size from this panel. The button is anchored to
  // the slot, so the slot collapses to nothing unless these stay non-zero.
  implicitWidth: Math.max(27, button.implicitWidth)
  implicitHeight: Math.max(28, button.implicitHeight)

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: "\uF0EB"
    tooltipText: "Hue"
    useActiveColor: false
    onPressed: function(b) { root.toggle() }
  }

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(400))
    contentHeight: panel.fittedContentHeight(column.implicitHeight)

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      blocked: ipField.activeFocus
      onCloseRequested: {
        if (root.openRoomId !== "") root.openRoomId = ""
        else root.close()
      }

      Column {
        id: column
        anchors.fill: parent
        spacing: Style.space(14)

        PanelHero {
          width: parent.width
          title: "Hue"
          meta: root.statusText
          foreground: root.uiFg
          fontFamily: root.uiFont
          iconComponent: Component {
            Text {
              text: "\uF0EB"
              color: root.uiFg
              font.family: root.uiFont
              font.pixelSize: Style.font.display
            }
          }
          trailingControl: root.paired ? forgetControl : null
        }

        Text {
          visible: root.lastError !== ""
          width: parent.width
          text: root.lastError
          wrapMode: Text.WordWrap
          textFormat: Text.PlainText
          color: root.uiUrgent
          font.family: root.uiFont
          font.pixelSize: Style.font.bodySmall
        }

        Column {
          visible: !root.paired
          width: parent.width
          height: visible ? implicitHeight : 0
          spacing: Style.space(10)

          Text {
            width: parent.width
            wrapMode: Text.WordWrap
            textFormat: Text.PlainText
            text: root.pairing
                  ? "Press the link button on the bridge. This keeps trying for 30 seconds."
                  : "Choose a bridge, or enter its address. Then press the link button."
            color: root.uiDim
            font.family: root.uiFont
            font.pixelSize: Style.font.bodySmall
          }

          Caption {
            visible: root.bridges.length > 0
            text: "BRIDGES"
          }

          Repeater {
            model: root.bridges
            delegate: Row {
              required property var modelData
              width: parent.width
              spacing: Style.space(8)

              Column {
                width: parent.width - bridgePair.implicitWidth - parent.spacing
                spacing: Style.space(2)
                Text {
                  width: parent.width
                  text: modelData.name || "Hue Bridge"
                  elide: Text.ElideRight
                  textFormat: Text.PlainText
                  color: root.uiFg
                  font.family: root.uiFont
                  font.pixelSize: Style.font.subtitle
                  font.bold: true
                }
                Text {
                  width: parent.width
                  text: modelData.ip || ""
                  elide: Text.ElideRight
                  textFormat: Text.PlainText
                  color: root.uiDim
                  font.family: root.uiFont
                  font.pixelSize: Style.font.caption
                }
              }

              Button {
                id: bridgePair
                text: "Pair"
                bordered: true
                enabled: !root.pairing
                foreground: root.uiFg
                fontFamily: root.uiFont
                fontSize: Style.font.bodySmall
                onClicked: root.pair(modelData.ip, modelData.id, modelData.name)
              }
            }
          }

          Caption { text: "ADDRESS" }

          Row {
            width: parent.width
            spacing: Style.space(8)

            TextField {
              id: ipField
              width: parent.width - manualPair.implicitWidth - parent.spacing
              placeholderText: "192.168.1.2"
              enabled: !root.pairing
              foreground: root.uiFg
              font.family: root.uiFont
              onAccepted: root.pair(text, "", "")
            }

            Button {
              id: manualPair
              text: root.pairing ? "Waiting" : "Pair"
              bordered: true
              enabled: !root.pairing && ipField.text.trim().length > 0
              foreground: root.uiFg
              fontFamily: root.uiFont
              fontSize: Style.font.bodySmall
              onClicked: root.pair(ipField.text, "", "")
            }
          }
        }

        Text {
          visible: root.paired && root.groups.length === 0 && root.lastError === ""
          width: parent.width
          wrapMode: Text.WordWrap
          textFormat: Text.PlainText
          text: "Nothing is set up on this bridge yet."
          color: root.uiDim
          font.family: root.uiFont
          font.pixelSize: Style.font.bodySmall
        }

        Flickable {
          id: scroller
          visible: root.paired && root.groups.length > 0
          width: parent.width
          height: visible ? Math.min(contentHeight, root.listCap) : 0
          contentWidth: width
          contentHeight: roomColumn.implicitHeight
          clip: true
          boundsBehavior: Flickable.StopAtBounds
          flickableDirection: Flickable.VerticalFlick

          ScrollBar.vertical: ScrollBar {
            policy: ScrollBar.AsNeeded
            interactive: false
            focusPolicy: Qt.NoFocus
          }

          Column {
            id: roomColumn
            width: scroller.width
            spacing: Style.space(16)

            Repeater {
              model: root.openRoomId === "" ? root.groups : []
              delegate: RoomRow {
                width: roomColumn.width
              }
            }

            RoomDetail {
              width: roomColumn.width
              visible: root.openRoomId !== "" && !!root.openRoom
              height: visible ? implicitHeight : 0
              room: root.openRoom
            }
          }
        }
      }
    }
  }

  component Caption: Text {
    textFormat: Text.PlainText
    color: root.uiDim
    font.family: root.uiFont
    font.pixelSize: Style.font.caption
    font.bold: true
    font.letterSpacing: 1.1
  }

  component ForgetButton: Button {
    text: root.forgetArmed ? "Confirm" : "Forget"
    bordered: true
    foreground: root.uiFg
    fontFamily: root.uiFont
    fontSize: Style.font.caption
    verticalPadding: Style.space(4)
    horizontalPadding: Style.space(8)
    onClicked: root.forget()
  }

  Component {
    id: forgetControl
    ForgetButton {}
  }

  component RoomRow: Item {
    id: roomRow
    required property var modelData
    required property int index
    readonly property int lamps: (modelData.lights || []).length
    readonly property bool drill: lamps > 0
    readonly property string roomName: modelData.name || (modelData.kind === "other" ? "Other" : "Room")

    width: parent ? parent.width : implicitWidth
    implicitHeight: rowBody.implicitHeight + (roomRow.index > 0 ? Style.space(12) : 0)

    PanelSeparator {
      visible: roomRow.index > 0
      width: parent.width
      anchors.top: parent.top
      foreground: root.uiFg
    }

    Row {
      id: rowBody
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.bottom: parent.bottom
      spacing: Style.space(8)

      Column {
        id: nameCol
        width: parent.width - (roomPower.visible ? roomPower.implicitWidth + parent.spacing : 0)
        spacing: Style.space(2)

        Text {
          width: parent.width
          text: roomRow.roomName + (roomRow.drill ? "  ›" : "")
          textFormat: Text.PlainText
          color: root.uiFg
          font.family: root.uiFont
          font.pixelSize: Style.font.subtitle
          font.bold: true
          elide: Text.ElideRight
        }

        Text {
          visible: roomRow.drill
          width: parent.width
          text: roomRow.lamps === 1 ? "1 lamp" : roomRow.lamps + " lamps"
          textFormat: Text.PlainText
          color: root.uiDim
          font.family: root.uiFont
          font.pixelSize: Style.font.caption
          elide: Text.ElideRight
        }
      }

      ToggleSwitch {
        id: roomPower
        visible: !!modelData.controlId
        checked: root.shown(modelData.controlId, "on", modelData.on === true)
        foreground: root.uiFg
        onToggled: root.toggleRoom(modelData, !root.shown(modelData.controlId, "on", modelData.on === true))
      }
    }

    MouseArea {
      anchors.left: parent.left
      anchors.top: rowBody.top
      anchors.bottom: rowBody.bottom
      width: nameCol.width
      enabled: roomRow.drill
      cursorShape: enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
      onClicked: root.showRoom(modelData.id)
    }
  }

  component RoomDetail: Column {
    id: roomDetail
    property var room: null
    spacing: Style.space(10)
    visible: !!room

    Button {
      text: room ? ("‹  " + (room.name || "Room")) : "‹"
      bordered: true
      foreground: root.uiFg
      fontFamily: root.uiFont
      fontSize: Style.font.bodySmall
      verticalPadding: Style.space(4)
      horizontalPadding: Style.space(10)
      onClicked: root.openRoomId = ""
    }

    Toggle {
      visible: !!room && room.kind === "room" && !!room.controlId
      width: parent.width
      label: room ? (room.name || "Room") : ""
      checked: !!room && root.shown(room.controlId, "on", room.on === true)
      foreground: root.uiFg
      fontFamily: root.uiFont
      onClicked: if (room) root.toggleRoom(room, !root.shown(room.controlId, "on", room.on === true))
    }

    Column {
      visible: !!room && room.supportsBrightness && root.shown(room.controlId, "on", room.on === true)
      width: parent.width
      height: visible ? implicitHeight : 0
      spacing: Style.space(6)

      Caption { text: "BRIGHTNESS" }
      HueSlider {
        width: parent.width
        bar: root.bar
        minimum: 1
        maximum: 100
        step: 1
        integer: true
        value: room ? root.shown(room.controlId, "brightness", room.brightness || 1) : 1
        onMoved: root.dragging = true
        onReleased: function(v) {
          root.dragging = false
          if (room) root.setBrightness("grouped_light", room.controlId, v)
        }
      }
    }

    Column {
      visible: !!(room && room.scenes && room.scenes.length)
      width: parent.width
      height: visible ? implicitHeight : 0
      spacing: Style.space(8)

      Caption { text: "SCENES" }
      Flow {
        width: parent.width
        spacing: Style.space(6)
        Repeater {
          model: (room && room.scenes) ? room.scenes : []
          delegate: Button {
            required property var modelData
            text: modelData.name
            bordered: true
            foreground: root.uiFg
            fontFamily: root.uiFont
            fontSize: Style.font.bodySmall
            verticalPadding: Style.space(4)
            horizontalPadding: Style.space(10)
            onClicked: root.activateScene(modelData)
          }
        }
      }
    }

    Column {
      visible: !!(room && room.lights && room.lights.length)
      width: parent.width
      height: visible ? implicitHeight : 0
      spacing: Style.space(8)

      Caption { text: "LIGHTS" }
      Repeater {
        model: (room && room.lights) ? room.lights : []
        delegate: Column {
          required property var modelData
          width: parent.width
          spacing: Style.space(8)

          Toggle {
            width: parent.width
            label: modelData.name || "Light"
            description: modelData.reachable === false ? "Offline"
                         : (root.shown(modelData.id, "on", modelData.on === true) && modelData.supportsBrightness
                            ? root.shown(modelData.id, "brightness", modelData.brightness || 1) + "%"
                            : "")
            checked: root.shown(modelData.id, "on", modelData.on === true)
            foreground: root.uiFg
            fontFamily: root.uiFont
            onClicked: root.toggleOn("light", modelData.id, !root.shown(modelData.id, "on", modelData.on === true))
          }

          LightExtras {
            light: modelData
          }
        }
      }
    }

    Column {
      visible: !!(room && room.switches && room.switches.length)
      width: parent.width
      height: visible ? implicitHeight : 0
      spacing: Style.space(8)

      Caption { text: "SWITCHES" }
      Repeater {
        model: (room && room.switches) ? room.switches : []
        delegate: Toggle {
          required property var modelData
          width: parent.width
          label: modelData.name || "Switch"
          description: modelData.reachable === false ? "Offline" : ""
          checked: root.shown(modelData.id, "on", modelData.on === true)
          foreground: root.uiFg
          fontFamily: root.uiFont
          onClicked: root.toggleOn("light", modelData.id, !root.shown(modelData.id, "on", modelData.on === true))
        }
      }
    }

    Column {
      visible: !!(room && room.sensors && room.sensors.length)
      width: parent.width
      height: visible ? implicitHeight : 0
      spacing: Style.space(8)

      Caption { text: "SENSORS" }
      Repeater {
        model: (room && room.sensors) ? room.sensors : []
        delegate: Item {
          required property var modelData
          width: parent.width
          implicitHeight: Math.max(sensorName.implicitHeight, sensorValue.implicitHeight)

          Text {
            id: sensorName
            width: parent.width - sensorValue.implicitWidth - Style.space(12)
            text: modelData.name || "Sensor"
            textFormat: Text.PlainText
            color: root.uiFg
            font.family: root.uiFont
            font.pixelSize: Style.font.subtitle
            font.bold: true
            elide: Text.ElideRight
          }

          Text {
            id: sensorValue
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            text: (modelData.reachable === false ? "Offline · " : "") + (modelData.value || "")
            textFormat: Text.PlainText
            color: root.uiDim
            font.family: root.uiFont
            font.pixelSize: Style.font.bodySmall
          }
        }
      }
    }
  }

  component LightExtras: Column {
    id: extras
    required property var light
    width: parent ? parent.width : implicitWidth
    visible: root.shown(light.id, "on", light.on === true)
              && (light.supportsBrightness || light.supportsColor || light.supportsColorTemperature)
    height: visible ? implicitHeight : 0
    spacing: Style.space(8)

    Caption {
      visible: light.supportsBrightness
      text: "BRIGHTNESS"
    }
    HueSlider {
      visible: light.supportsBrightness
      width: parent.width
      bar: root.bar
      minimum: 1
      maximum: 100
      step: 1
      integer: true
      value: root.shown(light.id, "brightness", light.brightness || 1)
      onMoved: root.dragging = true
      onReleased: function(v) {
        root.dragging = false
        root.setBrightness("light", light.id, v)
      }
    }

    Caption {
      visible: light.supportsColor
      text: "COLOR"
    }
    Flow {
      visible: light.supportsColor
      width: parent.width
      spacing: Style.space(6)
      Repeater {
        model: root.swatches
        delegate: Rectangle {
          required property var modelData
          width: Style.space(22)
          height: width
          radius: width / 2
          color: modelData.color
          border.width: 1
          border.color: Qt.rgba(root.uiFg.r, root.uiFg.g, root.uiFg.b, 0.4)

          MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: root.setHueSat(extras.light.id, modelData.hue, 100)
          }
        }
      }
    }
    HueSlider {
      visible: light.supportsColor
      width: parent.width
      bar: root.bar
      minimum: 0
      maximum: 360
      step: 1
      integer: true
      value: root.shown(light.id, "hue", light.hue || 0)
      onMoved: root.dragging = true
      onReleased: function(v) {
        root.dragging = false
        root.setHueSat(light.id, v, root.shown(light.id, "saturation", light.saturation || 100))
      }
    }
    Caption {
      visible: light.supportsColor
      text: "SATURATION"
    }
    HueSlider {
      visible: light.supportsColor
      width: parent.width
      bar: root.bar
      minimum: 0
      maximum: 100
      step: 1
      integer: true
      value: root.shown(light.id, "saturation", light.saturation || 100)
      onMoved: root.dragging = true
      onReleased: function(v) {
        root.dragging = false
        root.setHueSat(light.id, root.shown(light.id, "hue", light.hue || 0), v)
      }
    }

    Row {
      visible: light.supportsColorTemperature && light.mirekMax > light.mirekMin
      width: parent.width
      Text {
        id: coolLabel
        text: "COOL"
        textFormat: Text.PlainText
        color: root.uiDim
        font.family: root.uiFont
        font.pixelSize: Style.font.caption
        font.bold: true
        font.letterSpacing: 1.1
      }
      Item { width: Math.max(0, parent.width - coolLabel.width - warmLabel.width); height: 1 }
      Text {
        id: warmLabel
        text: "WARM"
        textFormat: Text.PlainText
        color: root.uiDim
        font.family: root.uiFont
        font.pixelSize: Style.font.caption
        font.bold: true
        font.letterSpacing: 1.1
      }
    }
    HueSlider {
      visible: light.supportsColorTemperature && light.mirekMax > light.mirekMin
      width: parent.width
      bar: root.bar
      minimum: light.mirekMin || 153
      maximum: light.mirekMax || 500
      step: 1
      integer: true
      value: root.shown(light.id, "ct", light.mirek || light.mirekMin || 153)
      onMoved: root.dragging = true
      onReleased: function(v) {
        root.dragging = false
        root.setCt(light.id, v)
      }
    }
  }
}
