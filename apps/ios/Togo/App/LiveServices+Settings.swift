import Foundation
import TogoKit

extension LiveServices {
    // MARK: Settings

    func applyStored(_ stored: AppSettings) {
        guard stored != settings else { return }
        applyingStoredSettings = true
        settings = stored
        applyingStoredSettings = false
    }

    /// The sync switch covers machines as well as settings; the settings store applies it to
    /// itself only. A switch flipped on another device reaches the host store here too.
    func settingsChanged(from old: AppSettings) {
        guard settings != old else { return }
        deps.shareSettings(settings)
        if settings.relayURL != old.relayURL || settings.liveActivities != old.liveActivities
            || settings.liveActivityCommandMachines != old.liveActivityCommandMachines {
            pairOnlineMachines()
        }
        if settings.liveActivityCommandMachines != old.liveActivityCommandMachines { sendCommandChoice() }
        for terminal in terminals.values {
            terminal.apply(settings)
        }
        let value = settings
        if value.syncEnabled != old.syncEnabled {
            let hostRecords = self.hostRecords
            Task { await hostRecords.setSyncEnabled(value.syncEnabled) }
        }
        guard !applyingStoredSettings else { return }
        let settingsStore = self.settingsStore
        // Saves run one after another, so a burst such as a pinch ends on its last value.
        let previous = lastSave
        lastSave = Task {
            await previous?.value
            await settingsStore.save(value)
        }
    }
}
