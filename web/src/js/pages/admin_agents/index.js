/* Agent Hub and in-page agent detail, served at the panel root and /{agent_id}. */

import { syncAdminAgentsMetadata } from './hub/hub.js';
import { init } from './init.js';
import { syncInpageNotesUI, handleInpageHeaderNoteClick, cancelInpageHeaderNoteEdit, saveInpageHeaderInlineNote, startInpageExpandedNotesEdit, cancelInpageExpandedNotesEdit, saveInpageExpandedNotes } from './editing/notes.js';
import { resetInpageEditStates, startInpageAgentNameEdit, cancelInpageAgentNameEdit, saveInpageAgentName } from './editing/rename.js';
import { handleAgentItemClick, navigateToAgent, closeAgentDetail } from './nav/routing.js';
import { setAgentViewMode, filterAgentsList, showInpageReinstallModal, showInpageUninstallModal } from './hub/view.js';
import { readPageConfig, snake } from '../../lib/page_config.js';

// Export module namespace
window.CertainStatsAdminAgents = {
  init: init,
  handleAgentItemClick: handleAgentItemClick,
  navigateToAgent: navigateToAgent,
  closeAgentDetail: closeAgentDetail,
  resetInpageEditStates: resetInpageEditStates,
  setAgentViewMode: setAgentViewMode,
  filterAgentsList: filterAgentsList,
  showInpageReinstallModal: showInpageReinstallModal,
  showInpageUninstallModal: showInpageUninstallModal,
  syncInpageNotesUI: syncInpageNotesUI,
  handleInpageHeaderNoteClick: handleInpageHeaderNoteClick,
  cancelInpageHeaderNoteEdit: cancelInpageHeaderNoteEdit,
  saveInpageHeaderInlineNote: saveInpageHeaderInlineNote,
  startInpageAgentNameEdit: startInpageAgentNameEdit,
  cancelInpageAgentNameEdit: cancelInpageAgentNameEdit,
  saveInpageAgentName: saveInpageAgentName,
  startInpageExpandedNotesEdit: startInpageExpandedNotesEdit,
  cancelInpageExpandedNotesEdit: cancelInpageExpandedNotesEdit,
  saveInpageExpandedNotes: saveInpageExpandedNotes,
  syncAdminAgentsMetadata: syncAdminAgentsMetadata
};

// Backwards compatibility globals for inline onclick handlers
window.handleAgentItemClick = handleAgentItemClick;
window.navigateToAgent = navigateToAgent;
window.closeAgentDetail = closeAgentDetail;
window.resetInpageEditStates = resetInpageEditStates;
window.setAgentViewMode = setAgentViewMode;
window.filterAgentsList = filterAgentsList;
window.showInpageReinstallModal = showInpageReinstallModal;
window.showInpageUninstallModal = showInpageUninstallModal;
window.handleInpageHeaderNoteClick = handleInpageHeaderNoteClick;
window.cancelInpageHeaderNoteEdit = cancelInpageHeaderNoteEdit;
window.saveInpageHeaderInlineNote = saveInpageHeaderInlineNote;
window.startInpageAgentNameEdit = startInpageAgentNameEdit;
window.cancelInpageAgentNameEdit = cancelInpageAgentNameEdit;
window.saveInpageAgentName = saveInpageAgentName;
window.startInpageExpandedNotesEdit = startInpageExpandedNotesEdit;
window.cancelInpageExpandedNotesEdit = cancelInpageExpandedNotesEdit;
window.saveInpageExpandedNotes = saveInpageExpandedNotes;

// The template renders this page's configuration into #page-config ahead of this script.
const pageConfig = readPageConfig('CertainStatsAdminAgents');
if (pageConfig) init({agents: snake(pageConfig.Agents || [])});
