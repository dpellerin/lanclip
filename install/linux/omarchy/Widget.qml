import QtQuick
import Quickshell
import Quickshell.Io
import qs.Ui

BarWidget {
    id: root
    implicitWidth: 22
    implicitHeight: vertical ? 20 : barSize
    property bool remote: false

    Accessible.name: remote ? "Lanclip, remote clipboard" : "Lanclip"

    Rectangle {
        anchors.centerIn: parent
        width: 21
        height: 17
        radius: 8
        color: "#33ffc738"
        visible: root.remote
    }

    Text {
        anchors.centerIn: parent
        text: "LC"
        textFormat: Text.PlainText
        font.pixelSize: 12
        font.weight: Font.DemiBold
        font.letterSpacing: -0.5
        color: root.remote ? "#ffc738" : "#a4a4a4"
    }

    Process {
        id: indicator
        command: [Quickshell.env("HOME") + "/.local/bin/lanclip", "indicator"]
        running: true
        stdout: SplitParser {
            onRead: line => {
                root.remote = JSON.parse(line).class === "remote";
            }
        }
        onExited: {
            root.remote = false;
            retry.start();
        }
    }

    Timer {
        id: retry
        interval: 2000
        onTriggered: indicator.running = true
    }
}
